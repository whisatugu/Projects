package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/csv"
	"encoding/json"
	"strconv"
	"container/list"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"
	"errors"
    "strings"
)

//Estrutura do BlockHeader
type BlockHeader struct {
	Index     int
	Timestamp string
	GameStateHash string
	TransactionsHash string
	Hash      string
	PrevHash  string
	Validator string
}

//Estrutura de cada Bloco da Blockchain
type Block struct {
	Head BlockHeader
	Transactions []Transaction
	GameState string
}
 
//Estrutura das Transações do jogo
type Transaction struct {
	Username string
	Commands []string
	SeqNumber int
}

//Estrutura do Cliente
type Client struct {
	Conn net.Conn
	Mutex sync.Mutex
	IP string
  	Messages chan string
}

//Estrutura do Cliente-jogador
type Player struct {
	//Campos iguais no jogo
	Username string
	PasswordHash string
	Ether int

	//Campos extras do server
	LastSeqNumber int
	C *Client
}

//Estrutura das mensagens
type JSONMessage struct {
	Keys []string
	Values []Player
}

//Estrutura para comunicação com jogo
type JSONPlayer struct {
    Username string
    PasswordHash string
    Ether int
}


//Constantes e Variaveis globais
const TIMEFORM = "2006-01-02 15:04:05.999999999 -0700"
const MAXCONNS = 100
const OPENCONNS = 3
const dCoinage = 61
var difficulty = 3
var TIMEOUT = 5 * time.Second
var secureBlock = 1 //Qual bloco (len - secureBlock) é considerado seguro
var gameState string //Game state do jogo

//Vetores/listas
var Blockchain []Block
var commandPool = list.New()
var transactionPool = list.New()
var tabelaServidores [2][100]string

//Flags
var forging bool
var logged bool

//Mutex para evitar condição de corrida
var mutex = &sync.Mutex{}
var mutexConnList = &sync.Mutex{}
var mutexTransactionPool = &sync.Mutex{}
var mutexCommandPool = &sync.Mutex{}

//Jogador conectado
var player Player

//MAPS
var localAddrs = make(map[string]int) //Guarda os endereços IP locais e os ja conectados para evitar conexões repetidas

var connectionsMap = make(map[string]*Client)

var transactionPoolMap = make(map[string]Transaction)

var playersMap = make(map[string]*Player)

//Channels
var unlockServer = make(chan int, 1)

var disconnect = make(chan int, 1)

var bet = make(chan int, 1) //Canal de anúncio de nova aposta

var ACK = make(chan int, 1) //Canal de sincronização de ACK

var att = make(chan int, 1) //Cria o canal de saída

var lose = make(chan Block) //Canal para anunciar que outro usuario validou o bloco

var loginChan = make(chan bool, 1) //Canal de anúncio de login de usuário


//----------------------------------Inicio Funções-------------------------------

func fillLocalAddrs() {
	//Pega a lista de enreços locais
	addrs, err := net.InterfaceAddrs()
	if err != nil {
	    panic(err)
	}
	//Adiciona no Map
	fmt.Println("Resgatando Ips locais")
	for i, addr := range addrs {
		ip := strings.SplitN(addr.String(), "/", 2)
		args := strings.SplitN(ip[0], ".", 3)
        if len(args) == 3 {
            fmt.Println("Ip local adicionado: ", ip[0])
            localAddrs[ip[0]] = i
        }
	}
}

//Função que cria um novo cliente, caso este ainda não exista no map de conexões
func newClient(conn net.Conn) (*Client) {
  	//Verifica se a conexão não é nula
  	if conn == nil {
      	return nil
    }
  
  	//Resgata o ip
  	aux := conn.RemoteAddr().String()
    remoteIP := strings.SplitN(aux, ":", 2)
  	ip := remoteIP[0]
  	
  	//Verifica se não é o ip de uma conexão vigente
    _, ok := connectionsMap[ip]
    if ok {
      fmt.Println("Conexão já estabelecida! Pulando")
      return nil
    }

  	//Cria o cliente
  	c := new(Client)
  	c.Conn = conn
  	c.IP = ip
  	c.Messages = make(chan string)
  
  	//Adiciona no map
  	mutexConnList.Lock()
  	connectionsMap[c.IP] = c
  	mutexConnList.Unlock()
  
  	return c
}

func closeClient(client *Client) {
	if client == nil  {
		return
	}
	ip := client.IP
	_, ok := connectionsMap[ip]
    if ok {
      //Se for o player
      if connectionsMap[ip] == player.C {
          //OBS: O ideal é obrigar a fazer login novamente
          logged = false
	  }
	  client.Conn.Close()
      //Remove da lista quem foi desconectado
      mutexConnList.Lock()
      delete(connectionsMap, ip)
      mutexConnList.Unlock()
      fmt.Println("Cliente desconectado!")
      fmt.Println("Número de clientes conectados: ", len(connectionsMap))

      //Se o número de clientes conectados for máximo
      if len(connectionsMap) == (MAXCONNS-1) {
        unlockServer <- 1
      }

    } else {
      	fmt.Println("Erro: Cliente não encontrado para a desconexão!")
    }
  }

//Verifica se o Hash atende a dificuldade da rede
func isHashValid(hash string, difficulty int) bool {
    prefix := strings.Repeat("0", difficulty)
    return strings.HasPrefix(hash, prefix)
}

//Função de validação dos blocos
func isBlockValid(newBlock, oldBlock Block) bool {
	if oldBlock.Head.Index+1 != newBlock.Head.Index {
		fmt.Println("Here 1")
		return false
	}

	if oldBlock.Head.Hash != newBlock.Head.PrevHash {
		fmt.Println("Here 2")
		return false
	}

	if calculateBlockHash(newBlock) != newBlock.Head.Hash {
		fmt.Println("Here 3")
		return false
	}

	//Caso não tenha validador
	if newBlock.Head.Validator == "" {
		fmt.Println("Here 4")
		return false
	}

	if isHashValid(newBlock.Head.Hash, calculateDifficultyPOS(newBlock, oldBlock)) == false {
		fmt.Println("Here 6")
		return false
	}

	return true
}

func verificaTransacao(t Transaction) (bool) {
	//Verifica se o player consta na lista de players existentes
	p, ok := playersMap[t.Username]
	if !ok {
		if t.SeqNumber == 0 { //Indica ser um novo player
			args := strings.SplitN(t.Commands[0], " ", 2)
			if len(t.Commands) == 1 && args[0] == "REGISTER" {
				return true
			}
			return false
		} else {
			fmt.Println("Player não existe")
			return false
		}
	}
	//Se não é vc mesmo (??!!)
	if (*p) == player {
		fmt.Println("Seria um clone maligno?! Ou apenas um eco da mensagem...")
		return false
	}
	//Se o número de sequência está correto e em ordem
	if p.LastSeqNumber >= t.SeqNumber {
		fmt.Println("Mensagem antiga perdida pela rede")
		return false
	}
	
	//Como a mensagem é menor, recebe o número da nova mensagem
	if p.LastSeqNumber < t.SeqNumber {
		playersMap[t.Username].LastSeqNumber = t.SeqNumber
	}

	return true
}


//Calcula a dificuldade do bloco, baseando-se no coinage
func calculateDifficultyPOS(newBlock Block, oldBlock Block) (int) {
	p := fmt.Println
	t, err1 := time.Parse(TIMEFORM, newBlock.Head.Timestamp)
	tOB, err2 := time.Parse(TIMEFORM, oldBlock.Head.Timestamp)
	var stack = 0

	//O validador do bloco deve ser sempre o primeiro
	if newBlock.Transactions != nil && newBlock.Transactions[0].Commands != nil {
		//Captura o Ether
		//Sempre a primeira transação do bloco
		args := strings.SplitN(newBlock.Transactions[0].Commands[0], " ", 2) //Para uma string BET NUMERO\n
		stack, _ = strconv.Atoi(strings.TrimSpace(args[1]))
		p("O Bet deste bloco é de: ", stack)
	}

	if err1 != nil && err2 != nil {
		p("Erro ocorrido no tempo")
	}
	
	//Faz a conta do coinage (adicionar score do validador)
	target := dCoinage - int(t.Sub(tOB).Seconds()) - (int(t.Sub(tOB).Seconds() * float64(stack)/10.0)) //Olhar essa conta
	//p(target)
	if target < 0 {
		target = 0
	}
	//p("A dificulade do bloco esta em: ", target)

	return target
}

//Retorna o ultimo estado salvo, a partir de um i passado
func getLastSavedGame(i int) (string){
	var json string
	json = ""
	for ; i < len(Blockchain) && i >= 0; i-- {
		if Blockchain[i].GameState != "" {
			json = Blockchain[i].GameState
			break
		}
	}
	return json
}

//Retorna o último numero de sequencia conhecido para aquele player
func getLastSeqNumber(username string) (int){
	//Percorre a Blockchain
	for i := len(Blockchain) - 1; i >= 0; i-- {
		//Percorre as transações de cada bloco
		for j := 0; j < len(Blockchain[i].Transactions); j++ {
			if Blockchain[i].Transactions[j].Username == username {
				return Blockchain[i].Transactions[j].SeqNumber
			}
		}
	}
	return -1
}

//Carrega lista de players de um JSON
func carregaPlayersFromJson(jsonGS string) bool {
	var m JSONMessage

	//Decodifica JSON
	err := json.Unmarshal([]byte(jsonGS), &m)
    if err != nil {
		fmt.Println("Error na decodificação do Json")
		return false
	} else {
		//Salva os players no vetor global de players
		for i := 0; i < len(m.Values); i++ {
			m.Values[i].LastSeqNumber = getLastSeqNumber(m.Values[i].Username)

			//OBS sujeito a alteração
			//Verifica possivel erro
			if m.Values[i].LastSeqNumber == -1{
				//log.Fatal("Erro Fatal: Player não encontrado, Blockchain inconsistente!\n(Para um player existir deve haver uma primeira transação de Registro de numero de sequencia 0)")
			}

			//Salva no map de players
			playersMap[m.Values[i].Username] = &m.Values[i]
		}
	}
	return true
}

//Carrega a lista de players extraída do último bloco e salva no Map
func carregaPlayersJson() {
	var m JSONMessage

	if len(Blockchain) - 1 == 0 {
		return
	}
	//Captura ultimo estado de jogo salvo na blockchain
	saved := getLastSavedGame(len(Blockchain) - 1)

	//Decodifica JSON
	err := json.Unmarshal([]byte(saved), &m)
    if err != nil {
		fmt.Println("Error na decodificação do Json")
	} else {
		//Salva os players no vetor global de players
		for i := 0; i < len(m.Values); i++ {
			m.Values[i].LastSeqNumber = getLastSeqNumber(m.Values[i].Username)

			//OBS sujeito a alteração
			//Verifica possivel erro
			if m.Values[i].LastSeqNumber == -1{
				//log.Fatal("Erro Fatal: Player não encontrado, Blockchain inconsistente!\n(Para um player existir deve haver uma primeira transação de Registro de numero de sequencia 0)")
			}

			//Salva no map de players
			playersMap[m.Values[i].Username] = &m.Values[i]
		}
	}
}

//Lê a blockchain salva no disco (se existir)
func carregaBlockchain() []Block{
	var BlockchainTemp []Block
	BlockchainTemp = nil
	buffer := make([]byte, 4096)
	//Abre os arquivos de bloco locais, decodifica, verifica integridade e salva na memória
	for i := 0; ; i++ {
		f, err := os.Open("Block_" + strconv.Itoa(i))
		if err == nil {
			n, err2 := f.Read(buffer)
			if err2 != nil{
				fmt.Println("Error: leitura de arquivo blockchain")
				return nil
			}
			//fmt.Println("lidos ", n)
			bloco := decodifica(buffer[:n])
			if i == 0 || isBlockValid(bloco, BlockchainTemp[i-1]) {
				BlockchainTemp = append(BlockchainTemp, bloco)
				imprimeBloco(bloco)
			} else {
				fmt.Println("Error: blockchain local salva inconsistente")
				break
			}
		} else {
			break
		}
		f.Close()
	}
	//Se i == 0, não existe blockchain local e sera retornado nil
	return BlockchainTemp
}

//Função que salva o bloco em arquivo
func salvaBloco(b Block) (error){
	vet := codifica(b)
	f, err := os.Create("Block_" + strconv.Itoa(b.Head.Index))
	defer f.Close()
	if err != nil {
		fmt.Println("Error: file creation fail in block saving")
		return err
	}

	_, err = f.Write(vet)
	if err != nil {
		fmt.Println("Error: write in file fail in block saving")
		return err
	}

	return nil
}

//Lê o arquivo csv e retorna uma matriz de strings
//TODO tratar para quando não há arquivo criado ainda
func parseData(file string) ([][]string, error) {
	f, err := os.Open(file)
    if err != nil {
        return nil, err
    }
    defer f.Close()

    data, err := csv.NewReader(f).ReadAll()
    if err != nil {
        return nil, err
    }

    return data, nil
}

//Gera um vetor com todas as transações da pool
func fromPoolToVectorT() ([]Transaction, bool) {
	var tVet []Transaction
	mutexTransactionPool.Lock()
	//Copia e converte os comandos para vetor
	for _, t := range transactionPoolMap {
    	tVet = append(tVet, t)
    }
    if len(tVet) == len(transactionPoolMap) {
		transactionPoolMap = make(map[string]Transaction) //Zera a pool
		mutexTransactionPool.Unlock()
	} else {
		fmt.Println("Erro: Comandos não foram convertidos e copiados, abortando!")
		mutexTransactionPool.Unlock()
		return tVet, false //TODO verificar os impactos deste return
	}

	return tVet, true
}

//Cria uma transação a partir dos comandos na pool de comandos
func transactionFromCommPool() (Transaction, bool) {
	var t Transaction
	fmt.Println("Player: ", player.Username)
	if player.Username == "" {
		return t, false
	}
	t.Username = player.Username
	t.SeqNumber = player.LastSeqNumber + 1
	player.LastSeqNumber++
	playersMap[player.Username].LastSeqNumber = player.LastSeqNumber

	mutexCommandPool.Lock()
	//Copia e converte os comandos para vetor
	for e := commandPool.Front(); e != nil; e = e.Next() {
    	t.Commands = append(t.Commands, e.Value.(string))
    }
    if (len(t.Commands) == commandPool.Len()) {
		commandPool.Init() //Zera a pool
		mutexCommandPool.Unlock()
	} else {
		fmt.Println("Erro: Comandos não foram convertidos e copiados, abortando!")
		mutexCommandPool.Unlock()
		return t, false //TODO verificar os impactos deste return
	}

	return t, true
}

//Resgata transações que estão perdidas
func salvaTransacoesPerdidas(newBlock Block, oldBlock Block){
	var hash string
	var hash2 string
	flag := false
	for _, t := range newBlock.Transactions {
		hash = calculateTransactionHash(t)
		_, ok := transactionPoolMap[hash]
		//Se a transação do novo bloco está na pool, apaga
		if ok {
			delete(transactionPoolMap, hash)
		}
	}
	for _, t := range oldBlock.Transactions {
		flag = false
		for _, t2 := range newBlock.Transactions {
			hash = calculateTransactionHash(t)
			hash2 = calculateTransactionHash(t2)
			if hash == hash2 {
				flag = true
				break
			}
		}
		if flag == false {
			transactionPoolMap[hash] = t
		}
	}
}


//----------------------------Funções de codificação/Decodificação--------------------------//

//Função do cálculo do hash em SHA256
func calculateHash(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	hashed := h.Sum(nil)
	return hex.EncodeToString(hashed)
}

//Função que calcula o hash do bloco, reunindo a informação necessária
func calculateBlockHash(block Block) string {
	t, _ := time.Parse(TIMEFORM, block.Head.Timestamp)
	record := string(block.Head.Index) + block.Head.GameStateHash + block.Head.TransactionsHash + block.Head.PrevHash + block.Head.Validator + string(t.Second())
	return calculateHash(record)
}

func calculateCommandsHash(commands []string) (string) {
	//Transforma os comandos em um vetor de bytes
	bytes := codificaComandos(commands)
	//Converte para string
	record := string(bytes)
	//Calcula o hash da string e retorna
	return calculateHash(record)
}

func calculateTransactionHash(t Transaction) (string){
    return calculateHash(string(codificaTransaction(t)))
}

func calculateTransactionsHash(t []Transaction) (string){
    //Codifica e verifica erro
	b, err := json.Marshal(t)
    if err != nil {
		log.Fatal("Error na decodificação do Json Transaction")
	}
    return calculateHash(string(b))
}

func codificaTransaction(t Transaction) ([]byte){
	//Codifica e verifica erro
	b, err := json.Marshal(t)
    if err != nil {
		log.Fatal("Error na decodificação do Json Transaction")
	}
	return b
}

func decodificaTransaction(b []byte) (Transaction){
	var t Transaction
	err := json.Unmarshal(b, &t)
    if err != nil {
		log.Fatal("Error na decodificação do Json Transaction")
	}
    return t
}

func codificaComandos(commands []string) ([]byte){
  	//Codifica e verifica erro
	b, err := json.Marshal(commands)
    if err != nil {
		fmt.Println("Error na decodificação do Json Comandos")
	}
	return b
}

func decodificaComandos(b []byte) (*list.List){
  	var respList = list.New()
	var commands []string
	err := json.Unmarshal(b, &commands)
    if err != nil {
		fmt.Println("Error na decodificação do Json Comandos")
	}
    for _, command := range commands {
    	//fmt.Println(command)
    	respList.PushBack(command)
    }

    return respList
}

func codificaTabela(mat [][]string) ([]byte){
	//Codifica e verifica erro
	b, err := json.Marshal(mat)
    if err != nil {
		fmt.Println("Error na decodificação do Json Tabela")
	}
	return b
}

func decodificaTabela(b []byte) ([][]string){
	var mat [][]string
	err := json.Unmarshal(b, &mat)
    if err != nil {
		fmt.Println("Error na decodificação do Json Tabela")
	}
    return mat
}

func codifica(block Block) ([]byte){
	b, err := json.Marshal(block)
    if err != nil {
		fmt.Println("Error na decodificação do Json Bloco")
	}
	return b
}

func decodifica(b []byte) (Block){
	var bloco Block
	err := json.Unmarshal(b, &bloco)
    if err != nil {
		fmt.Println("Error na decodificação do Json Bloco")
		fmt.Println(err)
	}
	return bloco
}

func imprimeBloco(b Block){
	fmt.Println("Bloco: ", b.Head.Index)
	fmt.Println("Timestamp: ", b.Head.Timestamp)
	fmt.Println("GameState Hash: ", b.Head.GameStateHash)
	fmt.Println("Transactions Hash: ", b.Head.TransactionsHash)
	fmt.Println("Hash: ", b.Head.Hash)
	fmt.Println("Previous Hash: ", b.Head.PrevHash)
	fmt.Println("Validador: ", b.Head.Validator)
	fmt.Println("GameState(JSON): ", b.GameState)
	if b.Transactions != nil {
		fmt.Println("Transactions in Block: ")
		for _, t := range b.Transactions {
			fmt.Println("	Transaction")
			fmt.Println("		Username: ", t.Username)
			fmt.Println("		SeqNumber: ", t.SeqNumber)
			fmt.Println("		Commands: ")
			if t.Commands != nil {
				for _, c := range t.Commands {
					fmt.Println("			", c)
				}
			}
		}
	} else {
		fmt.Println("Transactions in Block: Empty")
	}
	fmt.Println()
	fmt.Println()
}

//--------------------Funções de envio de mensagem----------------------//

//Envia mensagens que esperam uma mensagem de volta
func enviaMsg(conn net.Conn, msg string) (string, error){
	c1 := make(chan string, 1)
	var err error
	var resp string
	res := ""
	if conn != nil {
		fmt.Println("Enviando msg1: ", msg)
		rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
		rw.WriteString(msg)
		rw.Flush()

		err2 := &err

		go func() {
			//Aguarda confirmação de recebimento
    		fmt.Println("Aguardando recebimendo de ACK...1")
			resp, *err2 = rw.ReadString('\n')
			c1 <- resp
		}()
		if err != nil {
			fmt.Println("Error! ", err)
			return "", err
		}
		//Espera o retorno no tempo, caso contrário ocorre o timeout
		select {
    	case res = <-c1:
    		args := strings.SplitN(res, " ", 2)
			op := strings.TrimSpace(args[0])
    		if op == "OK" { //Tudo ocorreu corretamente
				fmt.Println(res)
				
			} else if strings.TrimSpace(res) == "ERRO" {
				fmt.Println("Erro: ", res)
				return "", errors.New(res)

			} else {
				fmt.Println(res)
			}
    	case <-time.After(TIMEOUT):
        	fmt.Println("Erro: client timed out!")
        	return "", errors.New("Client timed out!")
    	}

	} else {
		return "", errors.New("Cliente desconectado!")
	}

	return res, err
}

//Envia mensagens que esperam um ACK de OK ou ERRO no canal em vez de reader
func enviaMsg2(client *Client, msg string) (string, error){
	var err error
	err = nil
	res := ""
	if client == nil {
		fmt.Println("Erro: Cliente inexistente!")
		return res, errors.New("Cliente inexistente!")
	}
	if client.Conn != nil {
		fmt.Println("Enviando msg2: ", msg)
		rw := bufio.NewReadWriter(bufio.NewReader(client.Conn), bufio.NewWriter(client.Conn))
		rw.WriteString(msg)
		rw.Flush()
      
      	//Avisa que está esperando uma menssagem
      	client.Messages <- "ACK"

		fmt.Println("Aguardando recebimendo de ACK...2")
		//Espera o retorno no tempo, caso contrário ocorre o timeout
		select {
		
    	case res = <- client.Messages:
    		args := strings.SplitN(res, " ", 2)
			op := strings.TrimSpace(args[0])
    		if op == "OK" { //Tudo ocorreu corretamente
				fmt.Println(res)
			} else if strings.TrimSpace(res) == "ERRO" {
				fmt.Println("Erro: ", res)
			}
    	case <-time.After(TIMEOUT):
        	fmt.Println("Erro: client timed out!")
            closeClient(client)
        	return "", errors.New("Client timed out!")
    	}

	} else {
      	closeClient(client)
		return "", errors.New("Cliente desconectado!")
	}

	return res, err
}

//Envia mensagens que não esperam um ACK de volta
func enviaMsg3(client *Client, msg string) (error){
	var err error
	if client == nil {
		fmt.Println("Erro: Cliente inexistente!")
		return errors.New("Cliente inexistente!")
	}
	if client.Conn != nil {
		fmt.Println("Enviando msg3: ", msg)
		w := bufio.NewWriter(client.Conn)
		w.Reset(client.Conn)
		fmt.Println("Enviando3...")
		_, err := w.WriteString(msg)
		if err != nil {
			fmt.Println("Erro ao escrever string: " + msg)
		}
		w.Flush()
	} else {
      	closeClient(client)
		return errors.New("Cliente desconectado!")
	}

	return err
}
  
//Distribui mensagem para clientes conectados
func distribuiMsg(client *Client, msg string) {
  	fmt.Println("Distribuindo mensagem para os servidores")
	for key, enviar := range connectionsMap {
    	if enviar != client && enviar != player.C {
          	//Trava o Mutex para distribuição
    		enviar.Mutex.Lock()
    		if enviar.Conn != nil {
              fmt.Println("Enviando mensagem para o servidor: ", key)
				enviaMsg2(enviar, msg)
		    } else {
		    	fmt.Println("Cliente está desconectado")
		    }
    		enviar.Mutex.Unlock()
    	} 
	}
	fmt.Println("Distribuição finalizada!")
}

//Pode tb so receber a mensagem e repassa-la
func distribuiBloco(client *Client, bloco Block) {
	fmt.Println("Distribuindo bloco para os servidores")
	for key, enviar := range connectionsMap {
    	if enviar != client && enviar != player.C {
    		enviar.Mutex.Lock()
    		if enviar.Conn != nil {
				fmt.Println("Enviando bloco para o servidor: ", key)
              	msg := "WIN " + string(codifica(bloco)) + "\n"
              	enviaMsg3(enviar, msg)
		    } else {
		    	fmt.Println("Cliente está desconectado")
		    }
    		enviar.Mutex.Unlock()
    	} 
	}
	fmt.Println("Distribuição finalizada!")
}

//Flooda uma transação pela rede
func floodTransaction(client *Client, t Transaction) {
	//TRANSACTION BYTES\n
	bytes := codificaTransaction(t)
	msg := "TRANSACTION " + string(bytes) + "\n"
	distribuiMsg(client, msg)
}

//Envia os comando para o jogo e aguarda ACK nesta função
//OBS não pode ter lock
func enviaComandosParaJogo(bloco Block) {
	if len(Blockchain)-1 == 0 || player.C == nil{
		return
	}
	fmt.Println("Enviando comandos do Bloco " + strconv.Itoa(bloco.Head.Index) +" para o Jogo")
	for i := 0; i < len(bloco.Transactions); i++ {
		for j := 0; j < len(bloco.Transactions[i].Commands); j++ {
			if bloco.Transactions[i].SeqNumber !=0 { //Se é o registro muda
				msg := "COMMAND " + bloco.Transactions[i].Username + " " + bloco.Transactions[i].Commands[j] + "\n"
				enviaMsg2(player.C, msg)
			} else {
				msg := bloco.Transactions[i].Commands[j] + "\n"
				enviaMsg2(player.C, msg)
			}
		}
	}
	msg := "END BLOCK\n"
	enviaMsg3(player.C, msg)
	fmt.Println("Envio de comandos do Bloco finalizado!")
}

//Envia os comando para o jogo e aguarda ACK na função HANDLE
func enviaComandosParaJogo2(bloco Block) {
	if len(Blockchain)-1 == 0{
		return
	}
	fmt.Println("Enviando comandos do Bloco " + strconv.Itoa(bloco.Head.Index) +" para o Jogo")
	fmt.Println("----------LOCK em enviaComandosParaJogo2")
	player.C.Mutex.Lock()
	for i := 0; i < len(bloco.Transactions); i++ {
		for j := 0; j < len(bloco.Transactions[i].Commands); j++ {
			if bloco.Transactions[i].SeqNumber !=0 { //Se não é o registro
				msg := "COMMAND " + bloco.Transactions[i].Username + " " + strings.TrimSpace(bloco.Transactions[i].Commands[j]) + "\n"
				enviaMsg2(player.C, msg)
			} else {
				msg := strings.TrimSpace(bloco.Transactions[i].Commands[j]) + "\n"
				enviaMsg2(player.C, msg)
			}
		}
	}
	enviaMsg3(player.C, "END BLOCK\n")
	player.C.Mutex.Unlock()
	fmt.Println("==========UNLOCK em enviaComandosParaJogo2")
	fmt.Println("Envio de comandos do Bloco finalizado!")
}



//--------------------Funções de forjamento----------------------//

func generateGenesisBlock() {
	//Criando o genesisBlock
	var genesisBlock Block
	t := time.Now()
	var tr Transaction
	genesisBlock.Transactions = []Transaction{tr}
	genesisBlock.Transactions[0].Commands = []string{"let there be light"}
	//genesisBlock.GameState = "JSON"

	genesisBlock.Head.Index = 0
	genesisBlock.Head.Timestamp = t.Format(TIMEFORM)
	genesisBlock.Head.GameStateHash = "First of Many"
	genesisBlock.Head.TransactionsHash = "Create Genesis"
	genesisBlock.Head.PrevHash = "1"
	genesisBlock.Head.Validator = "God"
	genesisBlock.Head.Hash = calculateBlockHash(genesisBlock)
	Blockchain = []Block{genesisBlock};
	salvaBloco(genesisBlock)
}

// Cria um novo bloco para a blockchain
func generateBlock(oldBlock Block, name string, transactions []Transaction) (Block, error) {

	var newBlock Block

	if transactions == nil {
		//TODO Não sei oq fazer ainda
		//newBlock.Commands = []string{"BET 0\n"}
	} else {
		newBlock.Transactions = transactions
		//newBlock.Commands = commands
	}
	newBlock.GameState = gameState

	t := time.Now()
	newBlock.Head.Index = oldBlock.Head.Index + 1
	newBlock.Head.Timestamp = t.Format(TIMEFORM)
	newBlock.Head.GameStateHash = calculateHash(newBlock.GameState)
	newBlock.Head.TransactionsHash = calculateTransactionsHash(newBlock.Transactions)
	newBlock.Head.PrevHash = oldBlock.Head.Hash
	newBlock.Head.Validator = name
	newBlock.Head.Hash = calculateBlockHash(newBlock)

	return newBlock, nil
}

//Gera um novo bloco válido
func generatePOS() (Block, bool) {
	p := fmt.Println
	var newBlock Block
	var win bool
	win = true
	oldBlock := Blockchain[len(Blockchain)-1]
	var tVet []Transaction
	var ok bool

	//Verifica se existe um player logado
	if player.Username == "" {
		p("Erro: Não existe um player logado para forjar um bloco")
		return newBlock, false //TODO verificar os impactos deste return
	}

	for {
		//Gera um vetor com todas as transações da pool e zera esta
		tVet, ok = fromPoolToVectorT()
		if !ok {
			p("Erro: Transações não foram convertidos e copiados, abortando!")
			return newBlock, false //TODO verificar os impactos deste return
		}
		//Enquanto não houverem comandos (Sujeito a mudança)
		if len(tVet) > 0 {
			break
		}
	}

	p("Procurando Hash válido...")
	//Tenta encontrar o hash que satisfaça a dificulade da rede
	tempoI := time.Now()
	for tentativas := 1; ; tentativas++ {

		//Verifica se algum outro servidor já encontrou o bloco válido
		select {
          case bWinner := <-lose:
				p("XXXXXXXXXXXXXXXXXXXXXXXXXXX PERDEU XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX")
          		salvaTransacoesPerdidas(bWinner, newBlock)
				return newBlock, false
			default:
		}

		//TODO adicionar ID do jogador na geração dos blocos
		newBlock, _ = generateBlock(oldBlock, player.Username, tVet)

		target := calculateDifficultyPOS(newBlock, oldBlock)

		if isHashValid(newBlock.Head.Hash, target){
			//p("Hash satisfatório encontrado")
			//p(newBlock.Head.Hash)
			//p("Tentativas: ", tentativas)
			break
		} else {
			time.Sleep(time.Second - 100)
		}
	}

	if !isBlockValid(newBlock, oldBlock) {
		log.Fatal("Forjando blocos invalidos")
		return newBlock, false
	}

	p("--------------------------Bloco Forjado-------------------------")
	p("Levou: ", int(time.Now().Sub(tempoI).Seconds()))

	imprimeBloco(newBlock)
	return newBlock, win
}

//Função que mantém a geração de blocos
func forge() {
  	//Verifica se já não está ativa
  	if forging {
      	return
  	}
	forging = true
	for j := 1; ; j++ {
		novo, ok := generatePOS()
		if ok {
			mutex.Lock()
            if isBlockValid(novo, Blockchain[len(Blockchain)-1]) == true {
				Blockchain = append(Blockchain, novo)
				salvaBloco(novo)
				distribuiBloco(nil,novo)
				enviaComandosParaJogo2(novo)
			}
			mutex.Unlock()
        } else {
        	forging = false
          	return
        }
	}
}


//--------------------Principais funções do Cliente----------------------//

//Requisita toda a blockchain de outro servidor
func solicitaBlockchain(client *Client){
	var err error
	var res string

	//Envia o comando de solicitar todos os blocos ao servidor
	res, err = enviaMsg2(client, "BLOCK ALL\n")
	if err != nil {
		fmt.Println("Erouuu! ", err)
	}

	for {
		//Le o total enviado
		fmt.Println("Esperando próximo bloco ")

		if err != nil {
			fmt.Println("Erro: resposta fora do padrão, erro não tratado ou nula\n", err)
			return
		}
		//Espera o retorno no tempo, caso contrário ocorre o timeout
		//Quebra os argumentos
		args := strings.SplitN(res, " ", 2)
		//Limpa a msg
		op := strings.TrimSpace(args[0])
		//fmt.Println(op)
		if op == "END" {
			//Verifica se é a mensagem de confirmação de fim de operação e retorna
			fmt.Println("Blockchain sucessfully received! ")
			att <- 1
			return
		} else if op == "ERRO" {
			//TODO Verificar isso
			fmt.Println("Erro: erro de número ", args[1])
			return
		} else {
			bloco := decodifica([]byte(strings.TrimSpace(res)))
			fmt.Println("Bloco recebido:")
			imprimeBloco(bloco)
			//Decodifica o bloco e coloca na blockchain
			//mutex.Lock()
			if Blockchain == nil || isBlockValid(bloco, Blockchain[len(Blockchain)-1]) {
				fmt.Println("Bloco válido e inserido: " + strconv.Itoa(bloco.Head.Index))
				Blockchain = append(Blockchain, bloco)
				salvaBloco(bloco)
				res, err = enviaMsg2(client, "OK\n")
			} else {
				//Erro de recebimento de blockchain inconsistente
				fmt.Println("Erro de recebimento de blockchain inconsistente")
				enviaMsg3(client, "ERROR 0\n")
				return
			}
			//mutex.Unlock()
		}
	}
}

//Recupera a cadeia principal do servidor em caso onde estão incompatíveis
//O inteiro i representa a partir de qual index onde serão pedidos os blocos
func recuperaCadeiaPrincipal(client *Client, i int) (error) {
	var err error
	var res string
	err = nil
	fmt.Println("Entrando na função de recuperar cadeia...")

	//Blockchain temporária para realizar possíveis row backs
	var BlockchainTemp []Block
	var bloco Block
	res, err = enviaMsg2(client, "BLOCK BEFORE " + strconv.Itoa(i) + "\n")
	//Requisita blocos até encontrar a ligação com a blockchain local
	for ;i > 0 && i <= len(Blockchain); i-- {
		if err != nil {
			fmt.Println("Erro: ", err)
			break
		}
		args := strings.SplitN(res, " ", 2)
		op := strings.TrimSpace(args[0])
		if op == "OK" {
			//Continua executando o próximo bloco
			continue
		} else if op == "ERRO" {
			//TODO Verificar isso
			fmt.Println("Erro: erro de número ", args[1])
			return errors.New("erro de número " + args[1])
		}
		//Decodifica blcoo
		bloco = decodifica([]byte(op))

		res, err = enviaMsg2(client,"OK\n")

		//Salva o último bloco requerido na blockchain temporaria
		BlockchainTemp = append(BlockchainTemp, bloco)

		//Se verdade, encontramos de volta o elo de ligação com a cadeia existente local
		if isBlockValid(bloco, Blockchain[i-1]) == true {
			fmt.Println("Elo de ligação encontrado!")
			imprimeBloco(bloco)
			break
		}
	}

	//Após terminar o for, ou o bloco de ligação foi encontrado ou toda blockchain estava errada (pior caso)
	
	//Caso especial onde é encontrado de cara
	if len(Blockchain) == i {
		Blockchain = append(Blockchain, bloco)

	} else if i > 0 {
		//Retira da blockchain temporaria e coloca na local
		for j := len(BlockchainTemp) - 1; i < len(Blockchain) && j >= 0; j-- {
			if isBlockValid(BlockchainTemp[j], Blockchain[i-1]) == true {
              	salvaTransacoesPerdidas(BlockchainTemp[j], Blockchain[i])
				Blockchain[i] = BlockchainTemp[j]
				imprimeBloco(BlockchainTemp[j])
				salvaBloco(Blockchain[i])
				i++
			} else {
				fmt.Println("Erro: blockchain temporaria inconsistente")
				return err
			}
		}
	} else {
		//Neste caso mantém a blockchain local, pois algo errado ocorreu 
		fmt.Println("Erro: algo muito errado esta acontecendo! (possível ataque)")
		fmt.Println("Erro: gênesis block recebido não coincide")
		err = errors.New("Erro: gênesis block recebido não coincide")

	}
	return err
}

//Faz a requisição de novos blocos ao servidor conectado
//Chamada sempre que um servidor inicializa para fazer a atualização da blockchain
func atualizaBlockchain(client *Client){
	var err error
	var res string

	// Requisita do bloco de maior indice local até o mais recente validado no servidor
	//Caso exista blockchain local
	if Blockchain != nil && len(Blockchain) > 0 {
		inicio := Blockchain[len(Blockchain)-1].Head.Index + 1
		fmt.Println("Sera pedido a partir de ", inicio)
		res, err = enviaMsg2(client, "BLOCK SINCE " + strconv.Itoa(inicio) + "\n")
		if err != nil {
			fmt.Println("Erro: resposta fora do padrão, erro não tratado ou nula\n", err)
			closeClient(client)
			return
		}
		//Recebe os blocos, valida e os adicina na blockchain local
		for {
    		//Quebra os argumentos
    		args := strings.SplitN(res, " ", 2)
    		//Limpa a msg
			op := strings.TrimSpace(args[0])
			fmt.Println(op)
			if op == "END" {
				//Verifica se o envio já foi concluído
				//Neste caso retorna
				//fmt.Println("Entrou no END")
				fmt.Println("Recebimento de blocos realizado com sucesso!")
				return
			} else if op == "ERRO" {
				//TODO Verificar isso
				fmt.Println("Erro: erro de número ", args[1])
				closeClient(client)
				return
			} else {
				fmt.Println("Bloco recebido")
			}
			//Caso não, valida e adiciona o bloco
          	bloco := decodifica([]byte(strings.TrimSpace(res)))
			
			if isBlockValid(bloco, Blockchain[len(Blockchain)-1]) == true {
				Blockchain = append(Blockchain, bloco)
				salvaBloco(bloco)
				imprimeBloco(bloco)
				res, err = enviaMsg2(client, "OK\n")
				if err != nil {
					fmt.Println("Erro: resposta fora do padrão, erro não tratado ou nula\n", err)
					closeClient(client)
					return
				}
			} else {
				fmt.Println("Entrou no ELSE")
				//OBS: O único momento em que este else deve ocorrer é na primeira iteração do for

				//Trata erro de blockchain incompatível com a versão do servidor
				//Envia um erro para sinalizar o problema e encerra esta requisição (ERRO ou END? estou na duvida)
				//TODO: Fazer mapeamento correto de códigos de erro
				enviaMsg3(client, "END\n")
				
				//Faz a requisição de blocos anteriores até obter consistencia com o servidor
				if recuperaCadeiaPrincipal(client, inicio) == nil {
					//TODO se for problema de genesis block, deve-se cancelar conexão com o servidor
					atualizaBlockchain(client)
				}
				break
			}
		}
	} else {
		fmt.Println("Solicitando toda a Blockchain")
		//Caso a blockchain local não exista (primeira inicialização)
		solicitaBlockchain(client)
	}
}

//Conecta com os servidores
//Lê do arquivo contendo servidores conhecidos e o ID cadastrado neles
func openConn(){
	var err error
	flagN := true;
	var qtd int

	//Lê arquivo de servers para a memoria
	servers, err := parseData("servers.csv")
	if err != nil || len(servers) < 2 {
		fmt.Println("Erro: Abertura de arquivo de servers")
		return
	}

	//Cria um map com o números não repetidos, até a quantidade de servideores salvos, para testar na lista de servidores
	var zero = struct{}{}
	randomList := make(map[int32]struct{}, len(servers))
	for i := int32(0); i < int32(len(servers)); i++ {
        randomList[i] = zero
	}

	//Tenta estabelecer conexão com 3 (inicialmente) servers na lista
	if OPENCONNS > (len(servers) - 1) {
		qtd = len(servers) - 1
	} else {
		qtd = OPENCONNS
	}

	//For que tenta conectar até chegar ao qtd desejado
	for i := 0; i < qtd; i++ {
		//Recupera um IP de servidor da tabela e tenta conexão
		for j := range randomList {
			//Pula o 0
			if j == 0 {
				continue
			}

			//Separa o ip no ":"
			ip := strings.SplitN(servers[j][1], ":", 2)
			if len(ip) < 1 {
		        fmt.Println("Erro: IP lido fora do padrão")
		        return
		    }

		    //Verifica se o ip nao é o local
			_, ok := localAddrs[ip[0]]
			//Se for pula esta conexão
			if ok {
				fmt.Println("Ip local encontrado: ", ip[0])
				fmt.Println("Pulando conexão")
				continue
		    }

		    //Verifica se não é o ip de uma conexão vigente
		    mutexConnList.Lock()
		    _, ok = connectionsMap[ip[0]]
	 		//Caso seja pula a conexão
	 		if ok {
				fmt.Println("Pulando conexão")
				mutexConnList.Unlock()
	 			continue
	 		}
	 		mutexConnList.Unlock()

		    //Tenta a conexão
		    conn, err := net.DialTimeout("tcp", ip[0] + ":8080", TIMEOUT)//net.Dial("tcp", "192.168.0.35:8080")
		    //conn, err := net.Dial("tcp", servers[j][1])
			if err != nil {
			    netErr, ok := err.(net.Error)
			    //Verifica se a conexão não teve timeout
			    if ok && netErr.Timeout() {
			        //fmt.Println("Timed Out!")
			        continue
			    } else {
			    	fmt.Println("Error: ", err)
			    	continue
			    }
			} 

			//Neste caso a conexão foi bem sucedida
			//Double Check para conexão não nula
			if conn != nil {
				fmt.Println("Conected!")
				//Envia o comando de abertura
				res, err := enviaMsg(conn, "OPEN " + servers[j][0] + "\n")
				if err != nil {
					fmt.Println("Error: ", err)
					conn = nil
				}
				fmt.Println("Esperando resposta do servidor...")

	    		//Quebra os argumentos
	    		args := strings.SplitN(res, " ", 2)
	    		//Limpa a msg
				op := strings.TrimSpace(args[0])
				fmt.Println(op)

				if op == "OK" { //Obsoleto
					fmt.Println("Validado no servidor com sucesso!")

				} else if op == "ERRO" {
					//TODO Verificar isso
					fmt.Println("Erro: erro de número ", args[1])
					conn = nil

				} else {
					//Decodifica a tabela recebida
					novaServers := decodificaTabela([]byte(op))
					if len(novaServers) > len(servers) {
						//Abre arquivo
						f, err := os.Create("servers.csv")
					    if err != nil {
					    	fmt.Println("Erro: Alterando arquivo de servers")
					        return
					    }
					    
					    //Sobrescreve o arquivo de servers local
					    csv.NewWriter(f).WriteAll(novaServers)

					    f.Close()
					}
				}
				
				if (conn != nil){
					//Salva a conexão em um cliente do map
					c := newClient(conn)
					go clientListener(c, 1)

					fmt.Println("Atualizando blockchain...")
					//Ver se deixa assim msm
					c.Mutex.Lock()
					mutex.Lock()
					atualizaBlockchain(c)
					mutex.Unlock()
					c.Mutex.Unlock()

					flagN = false
					break
				}
			}
		}

		//Verifica se conseguiu se conectar a algum server
		if flagN == false {
			flagN = true
			continue
		} else {
			i--
			fmt.Println("Dormindo por 100 segundos")
			time.Sleep(100*time.Second)
		}
	}

    return
}

//Função que envia toda a Blockchain
func enviaBlockchain(client *Client){
	var err error
	//Envia blocos
	for _, bloco := range Blockchain {
		fmt.Println("Enviando Bloco:")
		imprimeBloco(bloco)
		//Escreve o bloco
		_, err = enviaMsg2(client, string(codifica(bloco)) + "\n")
		if err != nil {
			// handle error
			fmt.Println("Error: ", err)
			return
		}
	}

	//Escreve a msg de finalização de envio
	enviaMsg3(client, "END\n")
}

//Recebe o index de inicio e fim solicitado
func enviaBlocos(client *Client, inicio int, fim int){
	var err error
	end := fim

	if Blockchain == nil {
		fmt.Println("Erro: Blockchain local não existe")
		return
	}

	//Caso index fim -1, envia do inicio até o bloco atual
	if fim == -1 && inicio < len(Blockchain) {
		end = Blockchain[len(Blockchain)-1].Head.Index
		fmt.Println(inicio, end)
	}
	//Verifica possiveis erros de index (caso a blockchain nao esteja exatamente no index do vetor,
	//isto podera ser alterado)
	if fim > Blockchain[len(Blockchain)-1].Head.Index || inicio < Blockchain[0].Head.Index {
		fmt.Println("Erro: Requisição de bloco forma do index")
		return
	}
	//Envia os blocos
	for i := inicio; i <= end; i++ {
		imprimeBloco(Blockchain[i])
		//Escreve o bloco
		_, err = enviaMsg2(client, string(codifica(Blockchain[i])) + "\n")
		if err != nil {
			// handle error
			fmt.Println("Error: ", err)
			return
		}
		
	}

	//Caso inicio == fim, a mensagem de END não é esperada pelo cliente
	if inicio != fim {
		//Escreve a msg de finalização de envio
		enviaMsg3(client, "END\n")
	}
}

func open(client *Client, ID int) bool {
	flag := 0
	//buffer := make([]byte, 4096)

	//Lê arquivo de servers para a memoria
	data, err := parseData("servers.csv")
	if err != nil || len(data) < 2 {
		fmt.Println("Erro: Abertura de arquivo de servers")
		return false
	}
	
	//Procura pelo ip na tabela e valida
	ender := client.Conn.RemoteAddr().String()
	args := strings.SplitN(ender, ":", 2)
	ip := strings.TrimSpace(args[0])
	for i := 1; i < len(data); i++ {
		args2 := strings.SplitN(data[i][1], ":", 2)
		ipTable := strings.TrimSpace(args2[0])
		if ipTable == ip {
			fmt.Println("Achado na tabela")
			//Muda o flag pra indiacar que foi encontrado
			flag = 1
			break
		}
	}

	//Se não achou insere na tabela
	if flag == 0 {
		//Captura o ultimo id e incrementa
		newID, _ := strconv.Atoi(data[len(data)-1][0])
		newID++
		fmt.Println(newID)

		//Recupera o ip e porta do cliente
		d := []string{strconv.Itoa(newID), client.Conn.RemoteAddr().String()}
		fmt.Println(d)
		//Insere na tabela carregadada na memória
		data = append(data, d)
		//Abre arquivo
		f, err := os.Create("servers.csv")
	    if err != nil {
	    	fmt.Println("Erro: Abrindo arquivo")
	        return false
	    }
		//Salva alterações no arquivo
		fmt.Println(data)
		csv.NewWriter(f).WriteAll(data)
		f.Close()
	}

	//Envia tabela para o cliente
	enviaMsg3(client, string(codificaTabela(data)) + "\n")
	fmt.Println("Tabela enviada")
	return false
}

//------------------------------Principais funções do Player-------------------------//

func login(username string, key string, client *Client) {
	//Adiciona player na variável global
	p, ok := playersMap[username];
	if !ok {
		log.Fatal("Erro fatal: player não encontrado")
	}
	if p.PasswordHash != key {
		log.Fatal("Erro fatal: senha não confere")
	}
	player.Username = p.Username
	player.PasswordHash = key
	player.LastSeqNumber = p.LastSeqNumber
	player.C = client

	//Envia a mensagem de confirmação
	enviaMsg3(client, "OK\n")
	//loginChan <- true
  	logged = true
}

//Função que cuida dos procedimentos de entrada de novos players
func register(client *Client, msg string) (error) {
	var p JSONPlayer
	var err error

	args := strings.SplitN(msg, " ", 2)

	//Descodifica JSON em player para verificar se está válido
	err = json.Unmarshal([]byte(strings.TrimSpace(args[1])), &p)
	if err != nil {
		fmt.Println("Error na decodificação do Json")
		enviaMsg3(client, "ERROR 1\n") //Envia mensagem de erro cancelando o registro
		return err
	}

  	//Converte de JSONPLayer para Player
	var novoP Player
	novoP.Username = p.Username
	novoP.PasswordHash = p.PasswordHash
	novoP.Ether = p.Ether
	novoP.LastSeqNumber = 0
	//Adiciona player no map 
	playersMap[p.Username] = &novoP

	//Coloca em uma transação
	var t Transaction
	t.Username = p.Username
	t.SeqNumber = 0
	t.Commands = []string{msg}

	//Adiciona no inicio da pool de transações
	mutexTransactionPool.Lock()
	transactionPool.PushFront(t)
	transactionPoolMap[calculateTransactionHash(t)] = t
	mutexTransactionPool.Unlock()

	//Mensagem de confirmação
	enviaMsg3(client, "OK\n")

	return err
}

//Envia o último jogo salvo considerado seguro
func lastSavedGameState(client *Client){
	json := ""

	//Captura o estado do jogo no bloco seguro
	if len(Blockchain) > secureBlock {
		json = Blockchain[len(Blockchain) - secureBlock].GameState
	} else {
		json = Blockchain[len(Blockchain) - 1].GameState
	}
	if (json != ""){
		//Envia a mensagem com o JSON
		enviaMsg2(client, "GAMESTATE " + json + "\n");
	} else {
		//Caso existam inconsistencias neste bloco (verificar)
		fmt.Println("Erro: GameState inexistente no bloco seguro!")
		//Procura o último bloco que possui um gamestate e envia
		json = getLastSavedGame(len(Blockchain) - 1)
		if json != "" {
			fmt.Println("Encontrado GameState")
			enviaMsg2(client, "GAMESTATE " + json + "\n");
		} else {
			fmt.Println("Não foi encontrado GameState")
			enviaMsg3(client, "NOT FOUND\n");
		}
	}
}


//------------------------------Funções de listener-------------------------//

//Controla as conexões existentes (listener de novos clientes)
func clientServer(exit chan string){
	//Testanto conexão server
	ln, err := net.Listen("tcp", ":8080")
	if err != nil {
		// handle erro
		fmt.Println("Erro: Falha estabelecer porta para conexão")
	}

	for {
		//Aceita conexões até atingir o limite de MAXCONNS
		if len(connectionsMap) == MAXCONNS {
		  	<- unlockServer
		}
			
		for i:=0; len(connectionsMap) < MAXCONNS; i++ {
			fmt.Println("Esperando novos clientes...")

          	conn, err := ln.Accept()
          	if err != nil {
				// handle error
				fmt.Println("Erro: Falha estabelecer conexão")
				continue
			}
			
			aux := conn.RemoteAddr().String()
			fmt.Println("Conexão de: ", aux)
            remoteIP := strings.SplitN(aux, ":", 2)
            ip := remoteIP[0]
          	//Verifica se não é o player (conexão local)
          	_, ok := localAddrs[ip]
			if ok {
				fmt.Println("Erro de tentativa de ip local")
				continue
            }
          
			//Cria novo cliente
          	c := newClient(conn)
          	if c == nil {
				fmt.Println("Erro na criação de cliente")
              	closeClient(c)
                continue
            }

			//Chama função para cuidar da conexão
			go clientListener(c, i)
			fmt.Println("Cliente Conectado com sucesso!")
			fmt.Println("Número de clientes conectados: ", len(connectionsMap))
		}

		select {
	    	case <- exit: {
	      		return
	    	}
	    	default:
	    		continue
		}
	}
}

//Função que aceita e controla players conectados (limitado a 1)
func playerServer(exit chan string){
  	//Escuta a porta para conexão local
	ln, err := net.Listen("tcp", ":9090")
	if err != nil {
		// handle erro
		fmt.Println("Erro: Falha estabelecer porta para conexão")
	}
  	
    for {
        //Aceita conexões
        conn, err := ln.Accept()
        if err != nil {
          // handle error
          fmt.Println("Erro: Falha estabelecer conexão")
          continue

        }

        aux := conn.RemoteAddr().String()
        remoteIP := strings.SplitN(aux, ":", 2)
        ip := remoteIP[0]
        //Verifica se é o player (conexão local)
        _, ok := localAddrs[ip]
        if !ok {
          fmt.Println("Cliente não é local! Pulando conexão!")
          continue
        }

        c := newClient(conn)
        if c == nil {
          fmt.Println("Erro: Falha na criação de um novo Client")
          continue
        }

		player.C = c
		
		//Só aceita 1 por vez
		playerListener()

        select {
            case <- exit: {
              return
            }
        	default:
        		continue
        }
   	}
}

func clientListener(client *Client, i int) {
	r := bufio.NewReader(client.Conn)
	//Inicializa thread de tratamento de mensagem
	for{
		fmt.Println("\nAguardando requisição de cliente...")
		//Aguarda a requisição do cliente

		message, err := r.ReadString('\n')
		if err != nil {
			//Encerra a conexão caso o cliente caia/desconecte
			if err == io.EOF {
				fmt.Println("Erro: resposta nula")
			} else {	
				fmt.Println("Erro: ", err)
			}
			//Encerra conexão com cliente
			closeClient(client)
			return
		}
      	
      	//Verifica se alguma thread está esperando uma mensagem
      	select {
          	//Caso esteja envia para a thread
            case <- client.Messages:
            	client.Messages <- message

            default:
            	//Caso contrario envia mensagem recebida para o handle
      			go handleConnectionMsgs(client, message)
        }
	}
}

func playerListener() {
	if player.C == nil{
		fmt.Println("Erro: player desconectado!")
		closeClient(player.C)
		return
	}
	r := bufio.NewReader(player.C.Conn)
	//Inicializa thread de tratamento de mensagem
	for{
		fmt.Println("\nAguardando requisição do Player...")
		//Aguarda a requisição do cliente

		message, err := r.ReadString('\n')
		if err != nil {
			//Encerra a conexão caso o cliente caia/desconecte
			if err == io.EOF {
				fmt.Println("Erro: resposta nula")
			} else {	
				fmt.Println("Erro: ", err)
			}
			//Encerra conexão com cliente
			closeClient(player.C)
			return
		}
      	
      	//Verifica se alguma thread está esperando uma mensagem
      	select {
          	//Caso esteja envia para a thread
            case <- player.C.Messages:
            	fmt.Println("Enviando para thread")
            	player.C.Messages <- message

            default:
            	fmt.Println("Criando thread para requisição do player")
            	//Caso contrario envia mensagem recebida para o handle
      			go handlePlayerMsgs(message)
        }
	}
}


func handleConnectionMsgs(client *Client, message string) {
	//Trava o mutex para cuidar da requisição
  	client.Mutex.Lock()
    fmt.Println("Mensagem recebida do cliente: ", client.IP)
    fmt.Println("Conteúdo: ", message)

    //Separa a string no espaço
    args := strings.SplitN(message, " ", 2)
    if len(args) < 1 {
      fmt.Println("Erro: resposta fora do padrão")
      closeClient(client)
      client.Mutex.Unlock()
      return
    }

    //Remove os espaços e alguns lixos que tenham sobrado na string
    op := strings.TrimSpace(args[0])

    //Verifica a operação pedida pelo cliente e chama a função correta para atende-lo
    switch op {
		case "OPEN":
			fmt.Println("\nMensagem de abertura de conexão recebida!")
			id, _ := strconv.Atoi(strings.TrimSpace(args[1]))
			open(client, id)
		
		case "BLOCK":
			var inicio, fim int
			var conector string
			//Conta o número de espaços na substring restante para saber qual a mensagem
			n := strings.Count(args[1], " ")
			switch n {
				//String: BLOCK ALL\n
				//Caso em que todos os blocos são requeridos
				case 0:
					enviaBlockchain(client)

				case 1:
					//String: BLOCK SINCE 3\n
					//Caso peça de um determinado bloco até o atual
					if strings.SplitN(args[1], " ", 2)[0] == "SINCE" {
					fmt.Sscanf(args[1], "%s %d", &conector, &inicio)
					fmt.Println(message)
					enviaBlocos(client, inicio, -1)
					fmt.Println("Envio de blocos finalizado!")
					} else {
					//String: BLOCK BEFORE 3\n
					fmt.Sscanf(args[1], "%s %d", &conector, &inicio)
					fmt.Println(message)
					enviaBlocos(client, inicio, inicio)
					}

				//String: BLOCK 1 TO 3\n
				//Caso onde uma fatia dos blocos são requeridos
				case 2:
					fmt.Sscanf(args[1], "%d %s %d", &inicio, &conector, &fim)
					enviaBlocos(client, inicio, fim)
			}
		
		case "TRANSACTION":
			//Envia confirmação
			enviaMsg3(client, "OK\n")
			
			//Decodifica transação
			t := decodificaTransaction([]byte(strings.TrimSpace(args[1])))
		
			//Verifica a validade
			if verificaTransacao(t) {
				fmt.Println("Transação verificada")
				mutexTransactionPool.Lock()
				//Coloca no pool se estiver tudo correto
				transactionPool.PushBack(t)
				transactionPoolMap[calculateTransactionHash(t)] = t
				mutexTransactionPool.Unlock()
				//Distribui a mensagem para os demais clientes
				distribuiMsg(client, message)
			}

		case "WIN":
			fmt.Println("Recebido novo Bloco forjado")
			fmt.Println("================================= ", args)
			b := decodifica([]byte(strings.TrimSpace(args[1])))

			//Se for o próximo bloco esperado
			mutex.Lock()
			if isBlockValid(b, Blockchain[len(Blockchain)-1]) == true {
				//Se thread de forje viva, avisa que perdeu
				if forging {
					fmt.Println("Travado no Lose")
					lose <- b
					fmt.Println("Destravado no lose")
				}
				Blockchain = append(Blockchain, b)
				salvaBloco(b)
				gameState = getLastSavedGame(len(Blockchain) - 1)
				carregaPlayersFromJson(gameState)
				fmt.Println("________________________Bloco Vencedor________________________")
				imprimeBloco(b)
				go forge()

			//Se a cadeia principal local tiver sido ultrapassada
			} else if Blockchain[len(Blockchain)-1].Head.Index < b.Head.Index {
				fmt.Println("Bloco recebido maior que cadeia local")
				  //Se thread de forje viva, avisa que perdeu
				if forging {
					fmt.Println("Travado no Lose")
					lose <- b
					fmt.Println("Destravado no lose")
				}
				atualizaBlockchain(client)
				go forge()
			} else {
				fmt.Println("Bloco recebido menor que a cadeia existente")
			}
			mutex.Unlock()
		  
      //String: CLOSE CONECTION\n
      //Caso seja o encerramento da conexão
      case "CLOSE":
      	  closeClient(client)
      
      case "OK":
          select {
            case <- client.Messages:
            fmt.Println("WARNING: OK recebido no handle de requisicões!!")
            client.Messages <- message

            default:
            fmt.Println("ERROR: recebido OK fora de sincronia!!")
          }
    }
  	client.Mutex.Unlock()
}

func handlePlayerMsgs(message string) {
  	//Trava o mutex para cuidar da requisição
  	player.C.Mutex.Lock()
    fmt.Println("Mensagem recebida do player")
    fmt.Println("Conteúdo: ", message)

    //Separa a string no espaço
    args := strings.SplitN(message, " ", 2)
    if len(args) < 1 {
      fmt.Println("Erro: resposta fora do padrão")
	  closeClient(player.C)
	  player.C = nil
      return
    }

    //Remove os espaços e alguns lixos que tenham sobrado na string
    op := strings.TrimSpace(args[0])

    //Verifica a operação pedida pelo cliente e chama a função correta para atende-lo
    switch op {

		case "LOGIN":
			fmt.Println("Realizando o Login...")
			args2 := strings.SplitN(args[1], " ", 2)
			login(args2[0], strings.TrimSpace(args2[1]), player.C)
			fmt.Println("Login Concluido!")
			go forge()

		case "REGISTER":
			fmt.Println("Register")
			register(player.C, strings.TrimSpace(message))
			distribuiMsg(player.C, message)

		case "SAVED": //SAVED GAMESTATE\n
			//Envia o último jogo salvo ao cliente jogador
			lastSavedGameState(player.C)
			enviaComandosParaJogo(Blockchain[len(Blockchain)-1])

		case "COMMAND":
			fmt.Println("Recebido um novo comando do Jogo")
			if !logged {
				enviaMsg3(player.C, "ERROR 0\n") //Envia mensagem de ERROR não logado
			} else {
				fmt.Println("Enviando ACK de comando recebido")
				enviaMsg3(player.C, "OK\n") //Envia mensagem de confirmação
				mutexCommandPool.Lock()
				commandPool.PushBack(strings.TrimSpace(args[1])) //Coloca o comando no pool
				mutexCommandPool.Unlock()
			}

		case "GAMESTATE":
			fmt.Println("Recebido um GAMESTATE")
			var m JSONMessage
			//Decodifica a mensagem em JSON
			jsonS := strings.TrimSpace(args[1])
			err := json.Unmarshal([]byte(jsonS), &m)
			if err != nil {
				fmt.Println("Error na decodificação do Json")
			} else {
				gameState = jsonS
			}

			if carregaPlayersFromJson(gameState) {
				enviaMsg3(player.C, "OK\n")
			} else {
				fmt.Println("Erro: Erro no carregamento de players pelo gamestate recebido\nEnviando mensagem de erro")
				enviaMsg3(player.C, "ERROR 1\n")
			}

		case "BET":
			fmt.Println("Recebido um BET")
      		if !logged {
        		enviaMsg3(player.C, "ERROR 0\n") //Envia mensagem de ERROR não logado
      		} else {
				//Envia ACK
				enviaMsg3(player.C, "OK\n") //Envia mensagem de confirmação

				mutexCommandPool.Lock()
				commandPool.PushFront(strings.TrimSpace(message)) //Coloca o Bet no inicio do pool
				mutexCommandPool.Unlock()

				//Cria uma transação com os comandos
				t, ok := transactionFromCommPool()
				if !ok {
					log.Fatal("Erro: Comandos não foram convertidos e copiados, abortando!")
				}
				//Envia para os demais clientes
				fmt.Println("Enviando para outros players")
				floodTransaction(player.C, t)
				//Coloca transação como primeira na pool
				mutexTransactionPool.Lock()
				transactionPool.PushFront(t)
				transactionPoolMap[calculateTransactionHash(t)] = t
				mutexTransactionPool.Unlock()
				
				//bet <- 1
			}

		case "LOGOUT":
			closeClient(player.C)
			player.C = nil

		default:
            fmt.Println("Erro: mensagem inválida")
	}
	player.C.Mutex.Unlock()
}

func interfaceTeste() {
	var i int
	for{
		fmt.Scanf("%d", &i)
		switch i {
		case 1 :
			//openConn(conn)
			fmt.Println("Clientes conectados: ")
			for key, _ := range connectionsMap {
				fmt.Println("Cliente ", key)
	 		}
			fmt.Println("Fim de Operação")
		case 2 :

		case 3 :
			fmt.Println("Interfaces de rede do computador: ")
			addrs, err := net.InterfaceAddrs()
			if err != nil {
			    panic(err)
			}   
			for i, addr := range addrs {
			    fmt.Printf("%d %s\n", i, addr.String())
			}   
						//lose <- true
			fmt.Println("Fim de Operação")
		case 4 :
			fmt.Println("Comandos no pool: ")
			for c := commandPool.Front(); c != nil; c = c.Next() {
				fmt.Println(c.Value)
			}
			fmt.Println("Fim de Operação")

		case 5 :
			b := codificaComandos(Blockchain[len(Blockchain)-1].Transactions[0].Commands)
			fmt.Println("Codificação dos comandos do último bloco: ", b)
			fmt.Println("Decodificação: ")
			list := decodificaComandos(b)
			for e := list.Front(); e != nil; e = e.Next() {
		    	fmt.Println(e.Value.(string))
		    }
			fmt.Println("Fim de Operação")

		case 6 :
			msg:= "COMMAND " + player.Username + " CHANGE NOT_DEFINED 1 TO WORKER_STONE IN 0\n"
			fmt.Println("Enviando comando: " + msg + " do player: " + player.Username)
			player.C.Mutex.Lock()
			enviaMsg2(player.C, msg)
			player.C.Mutex.Unlock()

		case 7 :
			for _, p := range playersMap {
				fmt.Println("Lista de Players connhecidos:")
				fmt.Println("	Login ", p.Username)
				fmt.Println("	LastSegNumber ", p.LastSeqNumber)
			}
		
		case 8 :
			for t := transactionPool.Front(); t != nil; t = t.Next() {
				fmt.Println("Lista de Transações no pool:")
				fmt.Println("	Transaction")
				fmt.Println("		Username: ", t.Value.(*Transaction).Username)
				fmt.Println("		SeqNumber: ", t.Value.(*Transaction).SeqNumber)
				fmt.Println("		Commands: ")
				if t.Value.(Transaction).Commands != nil {
					for _, c := range t.Value.(Transaction).Commands {
						fmt.Println("			", c)
					}
				}
			}
		
		case 9:
			//enviaMsg2(player.C, "OK\n")
			
			/*
			var tt Transaction
			tt.Username = "caio"
			tt.SeqNumber = 9
			floodTransaction(player.C, Blockchain[len(Blockchain)-2].Transactions[0])
			*/
			//distribuiBloco(player.C, Blockchain[len(Blockchain)-2])
			fmt.Println(string(codifica(Blockchain[1])))

		default:

		}
	}
}

func main(){
	Blockchain = nil
	logged = false
	forging = false
	  

	//generateGenesisBlock()

	//Cria um canal de saída
	exit := make(chan string)

	//Carrega blockchain local na memória
	Blockchain = carregaBlockchain()

	fillLocalAddrs()

	go interfaceTeste()

	go openConn()

	//Caso a blockchain seja nula, nada pode ser feito até conseguir baixá-la de algum servidor
	if Blockchain == nil {
		//Função de solicitar blockchain escreve no channel
		<-att
	}

	go clientServer(exit)

	go playerServer(exit)

	go forge()

	//Verifica se a rotina terminou e encessa
	for {
	  select {
	    case <- exit: {
	      os.Exit(0)
	    }
	  }
	}


}
