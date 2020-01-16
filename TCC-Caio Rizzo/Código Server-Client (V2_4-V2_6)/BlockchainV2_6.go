package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/csv"
	"encoding/json"
	"strconv"
	"container/list"
//	"runtime/debug"
//	"encoding/json"
	"fmt"
	"io"
	"log"
//	"math/rand"
	"net"
	"os"
	"sync"
	"time"
	"errors"

	"bytes"
    "encoding/gob"
    "strings"

//	"github.com/davecgh/go-spew/spew"
//	"github.com/joho/godotenv"
)

// Block represents each 'item' in the blockchain
type BlockHeader struct {
	Index     int
	Timestamp string
	GameStateHash string
	CommandsHash string
	Hash      string
	PrevHash  string
	Validator string
}

type Block struct {
	Head BlockHeader
	Commands []string
	GameState string
}

type Client struct {
	Conn net.Conn
	Mutex sync.Mutex
	IP string
}

type Player struct {
	Username string
	Key string
	C *Client
}

//Constantes e Variaveis globais
//const TIMEFORM = "2006-01-02 15:04:05.999999999 -0700 MST" //Dando erro a parte do MST no final
const TIMEFORM = "2006-01-02 15:04:05.999999999 -0700"
const MAXCONNS = 100
const OPENCONNS = 3
const dCoinage = 61
var difficulty = 3
var TIMEOUT = 5 * time.Second
var clientesConectados = 0
var secureBlock = 3 //Qual bloco (len - secureBlock) é considerado seguro

// Blockchain is a series of validated Blocks
var Blockchain []Block
var tempBlocks []Block
var connections [MAXCONNS]Client
var connectionsList = list.New()
var commandPool = list.New()

//Jogador conectado
var player Player

//Game state do jogo
var gameState string

var tabelaJogo [10][10]string
var tabelaServidores [2][100]string

// candidateBlocks handles incoming blocks for validation
var candidateBlocks = make(chan Block)

// announcements broadcasts winning validator to all nodes
var announcements = make(chan string)

//Mutex para evitar condição de corrida
var mutex = &sync.Mutex{}
var mutexConnList = &sync.Mutex{}
var mutexCommandPool = &sync.Mutex{}

// Guarda os endereços IP locais e os ja conectados para evitar conexões repetidas
var localAddrs = make(map[string]int)

var connectionsMap = make(map[string]*Client)

//Canal para anunciar que outro usuario validou o bloco
var lose = make(chan string)

//Cria o canal de comunicação de saída para conexões
var close = make(chan string, 1)

//Cria o canal de saída
var att = make(chan int, 1)

//Canal de anúncio de nova aposta
var bet = make(chan int, 1)

// SHA256 hasing
// calculateHash is a simple SHA256 hashing function
func calculateHash(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	hashed := h.Sum(nil)
	//fmt.Println(hashed)
	return hex.EncodeToString(hashed)
}

//calculateBlockHash returns the hash of all block information
func calculateBlockHash(block Block) string {
	//record := string(block.Head.Index) + block.Head.Timestamp + block.Head.GameStateHash + block.Head.PrevHash
	t, _ := time.Parse(TIMEFORM, block.Head.Timestamp)
	record := string(block.Head.Index) + block.Head.GameStateHash + block.Head.CommandsHash + block.Head.PrevHash + block.Head.Validator + string(t.Second())
	return calculateHash(record)
}

// generateBlock creates a new block using previous block's hash
func generateBlock(oldBlock Block, address string, commands []string) (Block, error) {

	var newBlock Block

	if commands == nil {
		newBlock.Commands = []string{"BET 0\n"}
	} else {
		newBlock.Commands = commands
	}
	newBlock.GameState = gameState

	t := time.Now()
	newBlock.Head.Index = oldBlock.Head.Index + 1
	newBlock.Head.Timestamp = t.Format(TIMEFORM)
	newBlock.Head.GameStateHash = calculateHash(newBlock.GameState)
	newBlock.Head.CommandsHash = calculateCommandsHash(newBlock.Commands) //adicionar hash da tabela
	newBlock.Head.PrevHash = oldBlock.Head.Hash
	newBlock.Head.Validator = address
	newBlock.Head.Hash = calculateBlockHash(newBlock)

	return newBlock, nil
}

// isBlockValid makes sure block is valid by checking index
// and comparing the hash of the previous block
func isBlockValid(newBlock, oldBlock Block) bool {
	if oldBlock.Head.Index+1 != newBlock.Head.Index {
		return false
	}

	if oldBlock.Head.Hash != newBlock.Head.PrevHash {
		return false
	}

	if calculateBlockHash(newBlock) != newBlock.Head.Hash {
		return false
	}

	if isHashValid(newBlock.Head.Hash, calculateDifficultyPOS(newBlock, oldBlock)) == false {
		return false
	}

	return true
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

func calculateCommandsHash(commands []string) (string) {
	//Transforma os comandos em um vetor de bytes
	bytes := codificaComandos(commands)
	//Converte para string
	record := string(bytes)
	//Calcula o hash da string e retorna
	return calculateHash(record)
}

func codificaComandos(commands []string) ([]byte){
	// Initialize the encoder and decoder.  Normally enc and dec would be
    // bound to network connections and the encoder and decoder would
    // run in different processes.
    var network bytes.Buffer        // Stand-in for a network connection
    enc := gob.NewEncoder(&network) // Will write to network.
    //dec := gob.NewDecoder(&network) // Will read from network.
    // Encode (send) the value.
    err := enc.Encode(commands)
    if err != nil {
        log.Fatal("encode error:", err)
    }

    //Zera a lista de comandos atual
    //commandPool.Init()

    // HERE ARE YOUR BYTES!!!!
    //fmt.Println(network.Bytes())
    return network.Bytes()
}

func decodificaComandos(b []byte) (*list.List){
	// Decode (receive) the value
	var respList = list.New()
	var commands []string
	var network bytes.Buffer
	network.Write(b)
	dec := gob.NewDecoder(&network) // Will read from network.
    err := dec.Decode(&commands)
    if err != nil {
        log.Fatal("decode error:", err)
    }
    for _, command := range commands {
    	//fmt.Println(command)
    	respList.PushBack(command)
    }

    return respList
}

func codificaTabela(mat [][]string) ([]byte){
	// Initialize the encoder and decoder.  Normally enc and dec would be
    // bound to network connections and the encoder and decoder would
    // run in different processes.
    var network bytes.Buffer        // Stand-in for a network connection
    enc := gob.NewEncoder(&network) // Will write to network.
    //dec := gob.NewDecoder(&network) // Will read from network.
    // Encode (send) the value.
    err := enc.Encode(mat)
    if err != nil {
        log.Fatal("encode error:", err)
    }

    // HERE ARE YOUR BYTES!!!!
    //fmt.Println(network.Bytes())
    return network.Bytes();
}

func decodificaTabela(b []byte) ([][]string){
	// Decode (receive) the value
	var mat [][]string
	var network bytes.Buffer
	network.Write(b)
	dec := gob.NewDecoder(&network) // Will read from network.
    err := dec.Decode(&mat)
    if err != nil {
        log.Fatal("decode GameState error:", err)
    }

    return mat
}

func codifica(block Block) ([]byte){
	// Initialize the encoder and decoder.  Normally enc and dec would be
    // bound to network connections and the encoder and decoder would
    // run in different processes.
    var network bytes.Buffer        // Stand-in for a network connection
    enc := gob.NewEncoder(&network) // Will write to network.
    //dec := gob.NewDecoder(&network) // Will read from network.
    // Encode (send) the value.
    err := enc.Encode(block)
    if err != nil {
        log.Fatal("encode error:", err)
    }

    // HERE ARE YOUR BYTES!!!!
    //fmt.Println(network.Bytes())
    return network.Bytes();
}

func decodifica(b []byte) (Block){
	// Decode (receive) the value
	var block Block
	var network bytes.Buffer

	if b == nil {
		return block
	}

	network.Write(b)
	dec := gob.NewDecoder(&network) // Will read from network.
    err := dec.Decode(&block)
    if err != nil {
        log.Println("decode error:", err)
    }
    //fmt.Println(block.Head.Hash)
    return block
}

func imprimeBloco(b Block){
	fmt.Println("Bloco ", b.Head.Index)
	fmt.Println("Timestamp ", b.Head.Timestamp)
	fmt.Println("GameState Hash ", b.Head.GameStateHash)
	fmt.Println("Commands Hash ", b.Head.CommandsHash)
	fmt.Println("Hash ", b.Head.Hash)
	fmt.Println("Previous Hash ", b.Head.PrevHash)
	fmt.Println("Validador ", b.Head.Validator)
	fmt.Println("GameState(JSON) ", b.GameState)
	fmt.Println()
}

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

//Verifica se o Hash atende a dificuldade da rede
func isHashValid(hash string, difficulty int) bool {
    prefix := strings.Repeat("0", difficulty)
    return strings.HasPrefix(hash, prefix)
}

func generatePOW() (Block){
	p := fmt.Println
	var newBlock Block
	oldBlock := Blockchain[len(Blockchain)-1]
	t := time.Now()
	newBlock.Head.Index = oldBlock.Head.Index + 1
	newBlock.Head.Timestamp = t.Format(TIMEFORM)
	newBlock.Head.GameStateHash = "work on progress" //adicionar hash da tabela
	newBlock.Head.PrevHash = oldBlock.Head.Hash
	newBlock.Head.Validator = " "
	//form := "2019-04-23 18:18:23.1417948 -0300 STD"
	_,err := time.Parse(TIMEFORM, oldBlock.Head.Timestamp)
	if err != nil{
		p("Erro ocorrido no tempo")
	}
	
	
	p("Procurando Hash válido...")
	//Tenta encontrar o hash que satisfaça a dificulade da rede
	tentativas := 1
	for {
		t := time.Now()
		newBlock.Head.Timestamp = t.Format(TIMEFORM)
		/*
		target := 100 - int(t.Sub(tOB).Minutes())
		if target < 0 {
			target = 0
		}
		p("O target da rede esta em: ", target)
		target = 1
		p(target)
		*/
		newBlock.Head.Hash = calculateBlockHash(newBlock)
		if isHashValid(newBlock.Head.Hash, difficulty){
			p("Hash satisfatório encontrado")
			p("Tentativas: ", tentativas)

			break
		}
		tentativas++
	}
	return newBlock
}

//Fazer
func verificaBet(block Block, bet int) (bool){

	if block.Commands == nil {
		return false
	} else {
		//Captura o Ether
		//Sempre a primeira transação do bloco
		args := strings.SplitN(block.Commands[0], " ", 2) //Para uma string BET NUMERO\n
		stack, _ := strconv.Atoi(strings.TrimSpace(args[1]))
		if stack < 0 {
			fmt.Println("Stack inconsistente")
			return false
		} 

	}
	

	return true;
}

//TODO: Deve ser mexida no futuro, envolver o valor em moedas do validador
//Calcula a dificuldade do bloco, baseando-se no coinage
func calculateDifficultyPOS(newBlock Block, oldBlock Block) (int) {
	p := fmt.Println
	t, err1 := time.Parse(TIMEFORM, newBlock.Head.Timestamp)
	tOB, err2 := time.Parse(TIMEFORM, oldBlock.Head.Timestamp)
	var stack = 0

	if newBlock.Commands != nil {
		//Captura o Ether
		//Sempre a primeira transação do bloco
		args := strings.SplitN(newBlock.Commands[0], " ", 2) //Para uma string BET NUMERO\n
		stack, _ = strconv.Atoi(strings.TrimSpace(args[1]))
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

//Gera um novo bloco válido
func generatePOS() (Block, bool){
	p := fmt.Println
	var newBlock Block
	var win bool
	win = true
	oldBlock := Blockchain[len(Blockchain)-1]
	var commands []string

	//Se tiver um Bet coloca os comandos no bloco
	select {
		case <-bet:
			//Verificar se pode msm
			mutexCommandPool.Lock()
			//Copia e converte os comandos para vetor
			for e := commandPool.Front(); e != nil; e = e.Next() {
		    	commands = append(commands, e.Value.(string))
		    }
		    if (len(commands) == commandPool.Len()) {
				commandPool.Init() //Zera a pool
				mutexCommandPool.Unlock()
			} else {
				p("Erro: Comandos não foram convertidos e copiados, abortando!")
				mutexCommandPool.Unlock()
				return newBlock, false //TODO verificar os impactos deste return
			}
		default:
			//Continua o código com os comandos nulos
	}
	
	localAddr := GetOutboundIP() //Pega o endereço local para usar no campo validador (sujeito a mudança)

	p("Procurando Hash válido...")
	//Tenta encontrar o hash que satisfaça a dificulade da rede
	tempoI := time.Now()
	for tentativas := 1; ; tentativas++ {

		//Verifica se algum outro servidor já encontrou o bloco válido
		select {
			case <-lose:
				p("XXXXXXXXXXXXXXXXXXXXXXXXXXX PERDEU XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX")
				return newBlock, false
			default:
		}

		//Splita o endereço para pegar o IP
		localIP := strings.SplitN(localAddr, ":", 2)

		//TODO adicionar ID do jogador na geração dos blocos
		newBlock, _ = generateBlock(oldBlock, localIP[0], commands)

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
	p("--------------------------Bloco Forjado-------------------------")
	p("Levou: ", int(time.Now().Sub(tempoI).Seconds()))

	imprimeBloco(newBlock)
	return newBlock, win
}

func forge() {
	for j := 1; ; j++ {
		novo, ok := generatePOS()
		if ok {
			mutex.Lock()
            if isBlockValid(novo, Blockchain[len(Blockchain)-1]) == true {
				Blockchain = append(Blockchain, novo)
				salvaBloco(novo)
				distribuiBloco(nil,novo)
			}
			mutex.Unlock()
		}
	}
}


//--------------------Principais funções do Cliente----------------------//

/*
	-
*/

//Coleta o IP local
func GetOutboundIP() string {
    conn, err := net.Dial("udp", "8.8.8.8:80")
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()

    localAddr := conn.LocalAddr().String()

    return localAddr
}

//Conecta com os servidores
//Lê do arquivo contendo servidores conhecidos e o ID cadastrado neles
func openConn(){
	var err error
	flagN := true;
	buffer := make([]byte, 4096)
	var qtd int
	//reader := bufio.NewReader(conn)
	c1 := make(chan string, 1)

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

	//Captura o endereço local
	//localAddr := GetOutboundIP()

	for i := 0; i < qtd; i++ {
		for j := range randomList {
			//Recupera um IP de servidor da tabela e tenta conexão
			
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
			//localIP := strings.SplitN(localAddr, ":", 2)
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
				reader := bufio.NewReader(conn)
				//Envia o comando de abertura
				n, err := conn.Write([]byte("OPEN " + servers[j][0] + "\n"))
				if n == 0 || err != nil {
					fmt.Println("Error: escrevendo na conexão", err)
					conn = nil
				}
				fmt.Println("Esperando resposta do servidor...")

				//Criando ponteiros para que a thread possa mudar os valores
				n2 := &n
				err2 := &err
				
				go func() {
					//Recebe o ID ou uma mensagem de confirmação OK/ERRO
					*n2, *err2 = reader.Read(buffer)
					c1 <- string(buffer)
				}()
				if err != nil {
				    fmt.Println("Error: Lendo da conexão")
				    conn = nil
			    }
				//Espera o retorno no tempo, caso contrário ocorre o timeout
				select {
				case res := <-c1:
					//Extrai a mensagem
		    		clear := strings.SplitN(res, "\n", 2)
		    		//Quebra os argumentos
		    		args := strings.SplitN(clear[0], " ", 2)
		    		//Limpa a msg
					op := strings.TrimSpace(args[0])
					fmt.Println(op)

					if op == "OK" { //Obsoleto
						fmt.Println("Validado no servidor com sucesso!")

					} else if op == "ERRO" {
						//TODO Verificar isso
						fmt.Println("Erro: erro de número ", args[1])
						conn = nil

					} else if n == 0 {
						fmt.Println("Erro: recebido uma tabela nula")
						conn = nil
					} else {
						//Decodifica a tabela recebida
						novaServers := decodificaTabela(buffer)
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
				case <-time.After(TIMEOUT):
			    	fmt.Println("Erro: Connection timed out!")
			    	conn = nil
				}

				if (conn != nil){
					//Salva a conexão em um cliente do vetor
					var c Client
					c.Conn = conn
					c.IP = ip[0]
					mutexConnList.Lock()
					connectionsMap[ip[0]] = &c
					mutexConnList.Unlock()

					fmt.Println("Atualizando blockchain...")
					//Ver se deixa assim msm
					c.Mutex.Lock()
					mutex.Lock()
					atualizaBlockchain(c.Conn)
					mutex.Unlock()
					c.Mutex.Unlock()

					go handleConnection(&c, close, 1)


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

//TODO testar função
//Recupera a cadeia principal do servidor em caso onde estão incompatíveis
//O inteiro i representa a partir de qual index onde serão pedidos os blocos
func recuperaCadeiaPrincipal(conn net.Conn, i int) (error) {
	c1 := make(chan string, 1)
	var err error
	err = nil
	err2 := &err
	fmt.Println("Entrando na função de recuperar cadeia...")
	msg := make([]byte, 4096)
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	//Blockchain temporária para realizar possíveis row backs
	var BlockchainTemp []Block
	var bloco Block
	//Requisita blocos até encontrar a ligação com a blockchain local
	for ;i > 0 && i <= len(Blockchain); i-- {
		rw.WriteString("BLOCK BEFORE " + strconv.Itoa(i) + "\n")
		rw.Flush()

		go func() {
			_, *err2 = rw.Read(msg)
			c1<-string(msg)
		}()
		if err != nil {
			fmt.Println("Erro: resposta fora do padrão, erro não tratado ou nula\n", err)
			break
		}
		//Espera o retorno no tempo, caso contrário ocorre o timeout
		select {
    	case res := <-c1:
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
			//fmt.Println(res)
    	case <-time.After(TIMEOUT):
        	fmt.Println("Erro: Connection timed out! Função recuperaCadeiaPrincipal")
        	return errors.New("Connection timed out!")
    	}

		//Decodifica blcoo
		bloco = decodifica(msg)
		
		rw.WriteString("OK\n")
		rw.Flush()

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
func atualizaBlockchain(conn net.Conn){
	var err error
	msg := make([]byte, 4096)
	c1 := make(chan string, 1)
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	// Requisita do bloco de maior indice local até o mais recente validado no servidor
	//Caso exista blockchain local
	if Blockchain != nil && len(Blockchain) > 0 {
		inicio := Blockchain[len(Blockchain)-1].Head.Index + 1
		fmt.Println("Sera pedido a partir de ", inicio)
		rw.WriteString("BLOCK SINCE " + strconv.Itoa(inicio) + "\n")
		rw.Flush()

		//Recebe os blocos, valida e os adicina na blockchain local
		for {

			go func() {
				_, err = rw.Read(msg)
				c1 <- string(msg)
			}()
			if err != nil {
				fmt.Println("Erro: resposta fora do padrão, erro não tratado ou nula\n", err)
				return
			}
			//Espera o retorno no tempo, caso contrário ocorre o timeout
			select {
	    	case res := <-c1:
	    		//Extrai a mensagem
	    		clear := strings.SplitN(res, "\n", 2)
	    		//Quebra os argumentos
	    		args := strings.SplitN(clear[0], " ", 2)
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
					return
				} else {
					fmt.Println("Bloco recebido")
				}
	    	case <-time.After(TIMEOUT):
	        	fmt.Println("Erro: Connection timed out!")
	        	return
	    	}

			//Caso não, valida e adiciona o bloco
			bloco := decodifica(msg)
			
			if isBlockValid(bloco, Blockchain[len(Blockchain)-1]) == true {
				Blockchain = append(Blockchain, bloco)
				salvaBloco(bloco)
				imprimeBloco(bloco)
				rw.WriteString("OK\n")
				rw.Flush()
			} else {
				fmt.Println("Entrou no ELSE")
				//OBS: O único momento em que este else deve ocorrer é na primeira iteração do for

				//Trata erro de blockchain incompatível com a versão do servidor
				//Envia um erro para sinalizar o problema e encerra esta requisição (ERRO ou END? estou na duvida)
				//TODO: Fazer mapeamento correto de códigos de erro
				rw.WriteString("END\n")
				rw.Flush()
				
				//Faz a requisição de blocos anteriores até obter consistencia com o servidor
				if recuperaCadeiaPrincipal(conn, inicio) == nil {
					//TODO se for problema de genesis block, deve-se cancelar conexão com o servidor
					atualizaBlockchain(conn)
				}
				break
			}
		}
	} else {
		fmt.Println("Solicitando toda a Blockchain")
		//Caso a blockchain local não exista (primeira inicialização)
		solicitaBlockchain(conn)
	}
}

func solicitaBlockchain(conn net.Conn){
	buffer := make([]byte, 4096)
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	var err error
	var n int
	c1 := make(chan string, 1)

	//Envia o comando de solicitar todos os blocos ao servidor
	n, err = conn.Write([]byte("BLOCK ALL\n"))
	if n == 0 || err != nil {
		fmt.Println("Erouuu! ", err)
	}

	err2 := &err

	for {
		//Le o total enviado
		fmt.Println("Esperando próximo bloco ")

		go func() {
			_, *err2 = rw.Read(buffer)
			c1 <- string(buffer)
		}()
		if err != nil {
			fmt.Println("Erro: resposta fora do padrão, erro não tratado ou nula\n", err)
			return
		}
		//Espera o retorno no tempo, caso contrário ocorre o timeout
		select {
    	case res := <-c1:
    		//Extrai a mensagem
    		clear := strings.SplitN(res, "\n", 2)
    		//Quebra os argumentos
    		args := strings.SplitN(clear[0], " ", 2)
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
				bloco := decodifica(buffer)
				fmt.Println("Bloco recebido:")
				imprimeBloco(bloco)
				//Decodifica o bloco e coloca na blockchain
				//mutex.Lock()
				if Blockchain == nil || isBlockValid(bloco, Blockchain[len(Blockchain)-1]) {
					fmt.Println("Bloco válido e inserido: " + strconv.Itoa(bloco.Head.Index))
					Blockchain = append(Blockchain, bloco)
					salvaBloco(bloco)
					rw.WriteString("OK\n")
					rw.Flush()
				} else {
					//Erro de recebimento de blockchain inconsistente
					rw.WriteString("ERROR 0\n")
					rw.Flush()
				}
				//mutex.Unlock()
			}
    	case <-time.After(TIMEOUT):
        	fmt.Println("Erro: Connection timed out!")
        	return
    	}
	}
}



//--------------------Principais funções do servidor----------------------//


//Controla as conexões existentes, mantendo o limite de 3
func server(exit chan string){
	var ip string
	var err error
	i := 0
	l := &i //Ponteiro para i, para ser usado na goroutine
	//Testanto conexão server
	ln, err := net.Listen("tcp", ":8080")
	if err != nil {
		// handle erro
		fmt.Println("Erro: Falha estabelecer porta para conexão")
	}

	//Escuta as saídas dos clientes
	go func(){
		for {
			ip = <- close
			_, ok := connectionsMap[ip]
 			if ok {
 				//e.Close()
 				//Remove da lista quem foi desconectado
 				mutexConnList.Lock()
 				delete(connectionsMap, ip)
 				mutexConnList.Unlock()
 				//Libera mais uma vaga para clientes
 				if (*l) > 0 {
 					(*l)--
 				}
 				fmt.Println("Cliente desconectado!")
 				fmt.Println("Número de clientes conectados: ", len(connectionsMap))
 			} else {
 				fmt.Println("Erro: Cliente não encontrado para a desconexão!")
 			}
 		}
	}()

	for {
		//Aceita conexões até atingir o limite de MAXCONNS
		for i = 0; i < MAXCONNS; i++ {
			fmt.Println("Esperando novos clientes...")

			//Cria novo cliente
			var c Client

			//Aguarda nova conexão
			c.Conn, err = ln.Accept()
			if err != nil {
				// handle error
				fmt.Println("Erro: Falha estabelecer conexão")
 				i--
				continue
			}
			aux := c.Conn.RemoteAddr().String()
			remoteIP := strings.SplitN(aux, ":", 2)
			c.IP = remoteIP[0]

			//Verifica se não é o ip de uma conexão vigente
			_, ok := connectionsMap[c.IP]
			if ok {
				fmt.Println("Pulando conexão")
	 			c.Conn.Close()
	 			i--
	 			continue
			}

			//Adiciona na lista de clientes
			mutexConnList.Lock()
			connectionsMap[c.IP] = &c
			mutexConnList.Unlock()

			//Chama função para cuidar da conexão
			go handleConnection(&c, close, i)
			fmt.Println("Cliente Conectado com sucesso!")
			fmt.Println("Número de clientes conectados: ", len(connectionsMap))
		}

		select {
	    	case <- exit: {
	      		return
	    	}
		}
	}
}

//Receber um ID, carregar tabela servidores na memoria
func open(client *Client, ID int) (bool){
	flag := 0
	//buffer := make([]byte, 4096)

	//Lê arquivo de servers para a memoria
	data, err := parseData("servers.csv")
	if err != nil || len(data) < 2 {
		fmt.Println("Erro: Abertura de arquivo de servers")
		return false
	}
	w := bufio.NewWriter(client.Conn)
	
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
	w.Write(codificaTabela(data))
	w.Write([]byte("\n"))
	w.Flush()
	fmt.Println("Tabela enviada")
	return false
}

//Recebe o index de inicio e fim solicitado
func enviaBlocos(client *Client, inicio int, fim int){
	var err error
	var msg string
	c1 := make(chan string, 1)
	rw := bufio.NewReadWriter(bufio.NewReader(client.Conn), bufio.NewWriter(client.Conn))
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
		//Escreve o bloco
		imprimeBloco(Blockchain[i])
		fmt.Println(len([]byte(string(codifica(Blockchain[i])) + "\n")))
		num, _ := rw.Write(codifica(Blockchain[i]))
		rw.Write([]byte("\n"))
		fmt.Println(num)
		rw.Flush()

		go func() {
			//Aguarda confirmação de recebimento
			msg, err = rw.ReadString('\n')
			if err != nil {
				// handle error
				fmt.Println("Erro: resposta fora do padrão, erro não tratado ou nula\n", err)
			}
			c1 <- msg
		}()
		if err != nil {
			break
		}
		//Espera o retorno no tempo, caso contrário ocorre o timeout
		select {
    	case res := <-c1:
    		args := strings.SplitN(res, " ", 2)
			op := strings.TrimSpace(args[0])
			fmt.Println(op)
			if op == "OK" {
				//Continua executando o próximo bloco
				continue
			} else if op == "END" {
				//Neste caso retorna
				fmt.Println("Entrou no END")
				return
			} else if op == "ERRO" {
				//TODO Verificar isso
				fmt.Println("Erro: erro de número ", args[1])
				return
			}
			fmt.Println(res)
    	case <-time.After(TIMEOUT):
        	fmt.Println("Erro: clientection timed out!")
        	return
    	}
	}

	//Caso inicio == fim, a mensagem de END não é esperada pelo cliente
	if inicio != fim {
		//Escreve a msg de finalização de envio
		_, err := rw.WriteString("END\n")
		if err != nil {
			// handle error
			fmt.Println("Error! ", err)
		}
		rw.Flush()
	}
}

//OBS: Parece a melhor forma
//TODO: Trocar as mensagens de inicio de bloco e finalização de operação
func enviaBlockchain2(client *Client){
	c1 := make(chan string, 1)
	var err error
	var msg string
	//Envia blocos
	rw := bufio.NewReadWriter(bufio.NewReader(client.Conn), bufio.NewWriter(client.Conn))
	for _, bloco := range Blockchain {
		fmt.Println("Enviando Bloco:")
		imprimeBloco(bloco)
		//Escreve o bloco
		_, err = rw.WriteString(string(codifica(bloco)) + "\n")
		if err != nil {
			// handle error
			fmt.Println("Error! ", err)
		}
		rw.Flush()

		err2 := &err

		go func() {
			//Aguarda confirmação de recebimento
    		fmt.Println("Aguardadndo recebimendo de ACK...")
			msg, *err2 = rw.ReadString('\n')
			fmt.Println("mensagem recebida foi: ", msg)
			c1 <- msg
		}()
		if err != nil {
			fmt.Println("Error! ", err)
			break
		}
		//Espera o retorno no tempo, caso contrário ocorre o timeout
		select {
    	case res := <-c1:
    		if strings.TrimSpace(res) == "OK" {
				fmt.Println(res)
			} else {
				fmt.Println("Erro: mensagem inesperada recebida\nMensagem: ", res)
				return
			}
    	case <-time.After(TIMEOUT):
        	fmt.Println("Erro: clientection timed out!")
        	return
    	}
	}

	//Escreve a msg de finalização de envio
	_, err = rw.WriteString("END\n")
	if err != nil {
		// handle error
		fmt.Println("Error! ", err)
	}
	rw.Flush()
}

//TODO Testar função!
//Pode tb so receber a mensagem e repassa-la
func distribuiBloco(client *Client, bloco Block) {
	//c1 := make(chan string, 1)
	for key, enviar := range connectionsMap {
    	fmt.Println("Key:", key, "Value:", enviar)
    	if enviar != client {
    		enviar.Mutex.Lock()
    		if enviar.Conn != nil {
				fmt.Println("Enviando Bloco encontrado aos servidores conectados...")
				rw := bufio.NewReadWriter(bufio.NewReader(enviar.Conn), bufio.NewWriter(enviar.Conn))
				n, _ := rw.WriteString("WIN " + string(codifica(bloco)) + "\n")
				fmt.Println("Enviando %d bytes", n)
				rw.Flush()
		    } else {
		    	fmt.Println("Cliente está desconectado")
		    }
    		enviar.Mutex.Unlock()
    	} 
	}
}

//Envia mensagens que esperam um ACK de OK ou ERRO
func enviaMsg(client *Client, msg string) (error){
	c1 := make(chan string, 1)
	var err error
	var resp string
	if client.Conn != nil {
		rw := bufio.NewReadWriter(bufio.NewReader(client.Conn), bufio.NewWriter(client.Conn))
		rw.WriteString(msg)
		rw.Flush()

		err2 := &err

		go func() {
			//Aguarda confirmação de recebimento
    		fmt.Println("Aguardando recebimendo de ACK...")
			resp, *err2 = rw.ReadString('\n')
			c1 <- resp
		}()
		if err != nil {
			fmt.Println("Error! ", err)
			return err
		}
		//Espera o retorno no tempo, caso contrário ocorre o timeout
		select {
    	case res := <-c1:
    		args := strings.SplitN(res, " ", 2)
			op := strings.TrimSpace(args[0])
    		if op == "OK" { //Tudo ocorreu corretamente
				fmt.Println(res)
			} else if strings.TrimSpace(res) == "ERRO" {
				fmt.Println("Erro: ", res)
				return errors.New(res)

			} else {
				fmt.Println("Erro: mensagem inesperada recebida\nMensagem: ", res)
				return errors.New("Mensagem inesperada recebida")
			}
    	case <-time.After(TIMEOUT):
        	fmt.Println("Erro: client timed out!")
        	return errors.New("Client timed out!")
    	}

	} else {
		return errors.New("Cliente desconectado!")
	}

	return err
}

func distribuiMsg(client *Client, msg string) {

	for key, enviar := range connectionsMap {
    	fmt.Println("Key:", key, "Value:", enviar)
    	if enviar != client {
    		enviar.Mutex.Lock()
    		if enviar.Conn != nil {
				fmt.Println("Enviando mensagem aos servidores conectados...")
				enviaMsg(enviar, msg)
		    } else {
		    	fmt.Println("Cliente está desconectado")
		    }
    		enviar.Mutex.Unlock()
    	} 
	}
}

func register(client *Client, msg string) (error) {
	var p JSONPlayer
	var err error
	rw := bufio.NewReadWriter(bufio.NewReader(client.Conn), bufio.NewWriter(client.Conn))

	args := strings.SplitN(msg, " ", 2)

	//Descodifica JSON em player para verificar se está válido
	err = json.Unmarshal([]byte(strings.TrimSpace(args[1])), &p)
	if err != nil {
		fmt.Println("Error na decodificação do Json")
		rw.WriteString("ERROR 1\n") //Envia mensagem de erro cancelando o registro
		return err
	}

	//Mensagem de confirmação
	rw.WriteString("OK\n")
	rw.Flush()

	//Distribui a mensagem
	distribuiMsg(client, msg)

	return err
}

func login(username string, key string, client *Client) {
	w := bufio.NewWriter(client.Conn)

	//Adiciona player na variável global
	player.Username = username
	player.Key = key
	player.C = client

	//Envia a mensagem de confirmação
	w.WriteString("OK\n")
	w.Flush()

}

//Envia o último jogo salvo considerado seguro
func lastSavedGameState(client *Client){
	w := bufio.NewWriter(client.Conn)
	json := ""

	//Captura o estado do jogo no bloco seguro
	json = Blockchain[len(Blockchain) - secureBlock].GameState
	if (json != ""){
		//Envia a mensagem com o JSON
		w.WriteString("GAMESTATE " + json + "\n");
		w.Flush()
	} else {
		//Caso existam inconsistencias neste bloco (verificar)
		fmt.Println("Erro: GameState inexistente no bloco seguro!")

		//TESTE ALTERAR ESTE ELSE
		//Procura o último bloco que possui um gamestate e envia
		for i := len(Blockchain) - 1; i >= 0; i-- {
			if Blockchain[i].GameState != "" {
				fmt.Println("Enviando último gameState salvo encontrado")
				w.WriteString("GAMESTATE " + Blockchain[i].GameState + "\n");
				w.Flush()
				break
			}
		}
	}
	
}

func handleGameState(client *Client, jsonS string){
	var m JSONMessage
	w := bufio.NewWriter(client.Conn)

	//Decodifica a mensagem em JSON
	err := json.Unmarshal([]byte(jsonS), &m)
	if err != nil {
		fmt.Println("Error na decodificação do Json")
	} else {
		//Verifica se as credenciais batem com o player
		if m.Values[0].Username == player.Username && m.Values[0].PasswordHash == player.Key {
			//Seta o gameState para o recebido
			gameState = jsonS
		}
	}
	w.WriteString("OK\n")
	w.Flush()

}

//Mantém a conexão com o cliente e atende seus pedidos
func handleConnection(client *Client, close chan string, i int) {
	r := bufio.NewReader(client.Conn)
	for{
		fmt.Println("Aguardando requisição de cliente...")
		//Aguarda a requisição do cliente

		message, err := r.ReadString('\n')

		fmt.Println("Mensagem de cliente: ", client.IP)
		fmt.Println("Conteúdo: ", message)

		if err != nil {
			//Encerra a conexão caso o cliente caia/desconecte
			if err == io.EOF {
				fmt.Println("Erro: resposta nula")
			} else {	
				fmt.Println("Erro: ", err)
			}
			client.Conn.Close()
			close <- client.IP
			return
		}

		//Separa a string no espaço
		args := strings.SplitN(message, " ", 2)
        if len(args) < 1 {
            fmt.Println("Erro: resposta fora do padrão")
            close <- client.IP
            return
        }

        //Remove os espaços e alguns lixos que tenham sobrado na string
        op := strings.TrimSpace(args[0])

		//Verifica a operação pedida pelo cliente e chama a função correta para atende-lo
		switch op {

			case "LOGIN":
				args2 := strings.SplitN(args[1], " ", 2)
				client.Mutex.Lock()
				login(args2[0], strings.TrimSpace(args2[1]), client)
				client.Mutex.Unlock()
				
			//TODO
			case "REGISTER":
				register(client, message)

			case "SAVED": //SAVED GAMESTATE\n
				//Envia o último jogo salvo ao cliente jogador
				client.Mutex.Lock()
				lastSavedGameState(client)
				client.Mutex.Unlock()

			case "OPEN":
				id, _ := strconv.Atoi(strings.TrimSpace(args[1]))
				client.Mutex.Lock()
				open(client, id)
				client.Mutex.Unlock()
				fmt.Println(args[1])
				break

            case "BLOCK":
            	var inicio, fim int
            	var conector string
            	//Conta o número de espaços na substring restante para saber qual a mensagem
            	n := strings.Count(args[1], " ")
                switch n {
                    //String: BLOCK ALL\n
                    //Caso em que todos os blocos são requeridos
                    case 0:
                    	client.Mutex.Lock()
                    	enviaBlockchain2(client)
                    	client.Mutex.Unlock()
                    	
                    case 1:
                    	//String: BLOCK SINCE 3\n
                    	//Caso peça de um determinado bloco até o atual
                    	if strings.SplitN(args[1], " ", 2)[0] == "SINCE" {
                    		fmt.Sscanf(args[1], "%s %d", &conector, &inicio)
                    		fmt.Println(message)
                    		client.Mutex.Lock()
                    		enviaBlocos(client, inicio, -1)
                    		client.Mutex.Unlock()
                    		fmt.Println("Envio de blocos finalizado!")
                    	} else {
                    		//String: BLOCK BEFORE 3\n
                    		fmt.Sscanf(args[1], "%s %d", &conector, &inicio)
                    		fmt.Println(message)
                    		client.Mutex.Lock()
                    		enviaBlocos(client, inicio, inicio)
                    		client.Mutex.Unlock()
                    	}
                    	

                    //String: BLOCK 1 TO 3\n
                    //Caso onde uma fatia dos blocos são requeridos
                    case 2:
                    	fmt.Sscanf(args[1], "%d %s %d", &inicio, &conector, &fim)
                    	client.Mutex.Lock()
                    	enviaBlocos(client, inicio, fim)
                    	client.Mutex.Unlock()
                    	
                }

            case "WIN":
				fmt.Println("Recebido novo Bloco forjado")
            	b := decodifica([]byte(strings.TrimSpace(args[1])))

            	//TESTE
            	//Se for o próximo bloco esperado
            	mutex.Lock()
            	if isBlockValid(b, Blockchain[len(Blockchain)-1]) == true {
            		//distribuiBloco(conn, b)
            		lose <- "perdeu"
            		Blockchain = append(Blockchain, b)
					salvaBloco(b)
					fmt.Println("________________________Bloco Vencedor________________________")
					imprimeBloco(b)
					//fmt.Fprintf(conn, "OK\n")

				//Se a cadeia principal local tiver sido ultrapassada
            	} else if Blockchain[len(Blockchain)-1].Head.Index < b.Head.Index {
            		fmt.Println("Bloco recebido maior que cadeia local")
	            	atualizaBlockchain(client.Conn)
            	} else {
            		fmt.Println("Bloco recebido menor que a cadeia existente")
            	}
            	mutex.Unlock()

            case "COMMAND":
            	//Teste
            	fmt.Println("Recebido um novo comando do Jogo")

            	mutexCommandPool.Lock()
            	commandPool.PushBack(args[1]) //Coloca o comando no pool
            	mutexCommandPool.Unlock()
            	client.Mutex.Lock()
            	client.Conn.Write([]byte("OK\n")) //Envia mensagem de confirmação
            	client.Mutex.Unlock()

            case "BET":
            	bet <- 1

            case "GAMESTATE":
            	handleGameState(client, strings.TrimSpace(args[1]))


            //String: CLOSE CONECTION\n
            //Caso seja o encerramento da conexão
            case "CLOSE":
            	client.Mutex.Lock()
            	client.Conn.Close()
            	client.Mutex.Unlock()
            	fmt.Println("Conexão encerrada!")
            	close <- client.IP
            	return

            default:
            	fmt.Println("Erro: mensagem inválida")
            	return
        }

	}
}

//Estrutura das mensagens
type JSONMessage struct {
	Keys []string
	Values []JSONPlayer
}
type JSONPlayer struct {
    Username string
    PasswordHash string
    Ether int
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
			//solicitaBlockchain(conn)
			fmt.Println("IP local: ", GetOutboundIP())
			fmt.Println("Fim de Operação")
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
			b := codificaComandos(Blockchain[len(Blockchain)-1].Commands)
			fmt.Println("Codificação dos comandos do último bloco: ", b)
			fmt.Println("Decodificação: ")
			list := decodificaComandos(b)
			for e := list.Front(); e != nil; e = e.Next() {
		    	fmt.Println(e.Value.(string))
		    }
			fmt.Println("Fim de Operação")

		default:

		}
	}
}

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

func generateGenesisBlock() {
	//Criando o genesisBlock
	var genesisBlock Block
	t := time.Now()
	genesisBlock.Commands = []string{"let there be light"}
	genesisBlock.GameState = "JSON"

	genesisBlock.Head.Index = 0
	genesisBlock.Head.Timestamp = t.Format(TIMEFORM)
	genesisBlock.Head.GameStateHash = "First of Many"
	genesisBlock.Head.CommandsHash = "Create Genesis"
	genesisBlock.Head.PrevHash = "1"
	genesisBlock.Head.Validator = "0"
	genesisBlock.Head.Hash = calculateBlockHash(genesisBlock)
	Blockchain = []Block{genesisBlock};
	salvaBloco(genesisBlock)
}

func main(){
	Blockchain = nil

	//generateGenesisBlock()

	//Cria um canal de saída
	exit := make(chan string)

	//Carrega blockchain local na memória
	Blockchain = carregaBlockchain()

	fillLocalAddrs()

	go openConn()

	//Caso a blockchain seja nula, nada pode ser feito até conseguir baixá-la de algum servidor
	if Blockchain == nil {
		//Função de solicitar blockchain escreve no channel
		<-att
	}
	
	go server(exit)
	
	//Forjamento de Blocos
	go forge()
	
	go interfaceTeste()

	//debug.PrintStack()

	//time.Sleep(time.Second * 10)
	//lose <- "true"

	//Verifica se a rotina terminou e encessa
	for {
	  select {
	    case <- exit: {
	      os.Exit(0)
	    }
	  }
	}

}
