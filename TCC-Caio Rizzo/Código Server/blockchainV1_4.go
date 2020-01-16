package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/csv"
	"strconv"
//	"encoding/json"
	"fmt"
	"io"
	"log"
//	"math/rand"
	"net"
	"os"
	"sync"
	"time"

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
	TableHash string
	Hash      string
	PrevHash  string
	Validator string
}

type Block struct {
	Head BlockHeader
	Table string
}

type Client struct {
	Conn net.Conn
	Mutex sync.Mutex
}

//Constantes e Variaveis globais
const TIMEFORM = "2006-01-02 15:04:05.999999999 -0700 MST"
const MAXCONNS = 3
const dCoinage = 61
var difficulty = 3
var TIMEOUT = 5 * time.Second

// Blockchain is a series of validated Blocks
var Blockchain []Block
var tempBlocks []Block
var connections [MAXCONNS]Client
var hostsIP	[]byte

var tabelaJogo [10][10]string
var tabelaServidores [2][100]string

// candidateBlocks handles incoming blocks for validation
var candidateBlocks = make(chan Block)

// announcements broadcasts winning validator to all nodes
var announcements = make(chan string)

var mutex = &sync.Mutex{}

// validators keeps track of open validators and balances
var validators = make(map[string]int)

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
	//record := string(block.Head.Index) + block.Head.Timestamp + block.Head.TableHash + block.Head.PrevHash
	t, _ := time.Parse(TIMEFORM, block.Head.Timestamp)
	record := string(block.Head.Index) + block.Head.TableHash + block.Head.PrevHash + block.Head.Validator + string(t.Second())
	return calculateHash(record)
}

// generateBlock creates a new block using previous block's hash
func generateBlock(oldBlock Block, address string) (Block, error) {

	var newBlock Block

	t := time.Now()

	newBlock.Head.Index = oldBlock.Head.Index + 1
	newBlock.Head.Timestamp = t.Format(TIMEFORM)
	newBlock.Head.TableHash = "work on progress" //adicionar hash da tabela
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

//Controla as conexões existentes, mantendo o limite de 3
func server(exit chan string){
	var empty int
	var err error
	//Cria o canal de comunicação de saída
	close := make(chan int)
	//Testanto conexão server
	ln, err := net.Listen("tcp", ":8080")
	if err != nil {
		// handle erro
		fmt.Println("Erro: Falha estabelecer porta para conexão")
	}

	//Aceita conexões até atingir o limite de 3
	for i, _ := range connections {
		connections[i].Conn, err = ln.Accept()
		if err != nil {
			// handle error
			fmt.Println("Erro: Falha estabelecer conexão")
		}
		go handleConnection(connections[i], close, i)
	}

	//Organiza as saídas e entradas de novas conexões após o vetor estar completo
	//TODO: Conseguir uma solução melhor 
	//OBS (pior caso 2 threads em livinglock (procurar na ducomentacao golang em chanels))
	for{
		empty = <- close
		connections[empty].Conn, err = ln.Accept()
		if err != nil {
			fmt.Println("Erro: Falha estabelecer conexão")
			close <- empty
			continue
		}
		go handleConnection(connections[empty], close, empty)
	}

	exit <- "sair"
}

func login(user string, key string){

}

//Receber um ID, carregar tabela servidores na memoria
//Verificar se ID existe, se sim retorna true, se nao false
//Em caso de ID == 0, cadastra na tabela e envia para cliente
//Salva alterações, caso existam, no arquivo
func open(client Client, ID int) (bool){
	//Lê arquivo de servers para a memoria
	data, err := parseData("servers.csv")
	if err != nil || len(data) < 2 {
		fmt.Println("Erro: Abertura de arquivo de servers")
		return false
	}
	w := bufio.NewWriter(client.Conn)
	//Captura o ultimo id e incrementa
	newID, _ := strconv.Atoi(data[len(data)-1][0])
	newID++
	fmt.Println(newID)
	//Caso o ID nao esteja cadastrado
	if ID == 0 {
		d := []string{strconv.Itoa(newID), client.Conn.RemoteAddr().String()}
		fmt.Println(d)
		data = append(data, d)
		//Abre arquivo
		f, err := os.Create("servers.csv")
	    if err != nil {
	    	fmt.Println("Erro: Abrindo arquivo")
	        return false
	    }
	    defer f.Close()
		//Salva alterações
		fmt.Println(data)
		csv.NewWriter(f).WriteAll(data)
		//Envia ID para cliente
		w.WriteString(strconv.Itoa(newID) + "\n")
		w.Flush()
		return true
	} else {
		//Caso esteja, procura por ele na tabela e valida
		for i := 1; i < len(data); i++ {
			if data[i][1] == string(ID) {
				return true
			}
		}
		w.WriteString("OK\n")
		w.Flush()
	}

	return false
}

//Recebe o index de inicio e fim solicitado
func enviaBlocos(client Client, inicio int, fim int){
	var err error
	var msg string
	c1 := make(chan string, 1)
	rw := bufio.NewReadWriter(bufio.NewReader(client.Conn), bufio.NewWriter(client.Conn))
	end := fim
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
func enviaBlockchain2(client Client){
	c1 := make(chan string, 1)
	var err error
	var msg string
	//Envia blocos
	rw := bufio.NewReadWriter(bufio.NewReader(client.Conn), bufio.NewWriter(client.Conn))
	for _, bloco := range Blockchain {
		//Escreve o bloco
		_, err = rw.WriteString(string(codifica(bloco)) + "\n")
		if err != nil {
			// handle error
			fmt.Println("Error! ", err)
		}
		rw.Flush()

		go func() {
			//Aguarda confirmação de recebimento
    		fmt.Println("Aguardadndo recebimendo de ACK...")
			msg, err = rw.ReadString('\n')
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

//Envia toda a blockchain a um cliente
func enviaBlockchain(client Client){
	//buffer := make([]byte, 4096)
	//Envia 
	//writer := bufio.NewWriter(client)
	//reader := bufio.NewReader(client)
	for _, bloco := range Blockchain {
		//envia o bloco junto com o código de bloco (melhorar depois)
		fmt.Println("Enviando bloco!")
		//fmt.Fprintf(client, "a" + string(codifica(bloco)))
		
		n, err := client.Conn.Write([]byte("a" + string(codifica(bloco))))
		if n == 0 || err != nil {
			fmt.Println("Erouuu! ", err)
		}

		//data := codifica(bloco)
		//writer.WriteString("a" + string(data))
		//writer.Reset(writer)
		fmt.Println("Esperando confirmação")
		msg, err := bufio.NewReader(client.Conn).ReadString('\n')
		if err != nil && err != io.EOF {
			// handle error
			fmt.Println("Error! ", err)
			break
		}
		fmt.Println(msg)
	}
	//envia o código de finalizaçao
	//writer.WriteString("b\n")
	//writer.Flush()
	/*
	n, err := client.Write([]byte{'b'})
	if n == 0 || err != nil {
		fmt.Println("Erouuu! ", err)
	}
	*/
	fmt.Fprintf(client.Conn, "b")
	fmt.Println("Entrega de blockchain concluída!")
}

/*
func espalhaBloco(client Client, bloco Block){
	for _, enviar := range connections {
		if client.Conn.RemoteAddr().String() != enviar.RemoteAddr().String() {

		}
	}
}
*/

//TODO Testar função!
//Pode tb so receber a mensagem e repassa-la
func distribuiBloco(client Client, bloco Block) {
	//c1 := make(chan string, 1)
	if client.Conn == nil {
		for i, enviar := range connections {
			if enviar.Conn != nil {
				enviar.Mutex.Lock()
				fmt.Println("Enviando Bloco encontrado aos servidores conectados...")
				rw := bufio.NewReadWriter(bufio.NewReader(enviar.Conn), bufio.NewWriter(enviar.Conn))
				rw.WriteString("WIN " + string(codifica(bloco)) + "\n")
				rw.Flush()
				enviar.Mutex.Unlock()
		    } else {
		    	fmt.Println("Vazia a conexão ", i)
		    }
    	}
	}
}

//Mantém a conexão com o cliente e atende seus pedidos
func handleConnection(client Client, close chan int, i int) {
	r := bufio.NewReader(client.Conn)
	for{
		fmt.Println("Aguardando requisição de cliente...")
		//Aguarda a requisição do cliente

		message, err := r.ReadString('\n')

		//Encerra a conexão caso o cliente caia/desconecte
		if err == io.EOF {
			fmt.Println("Erro: resposta nula")
			client.Conn.Close()
			close <- i
			return
		}
		if err != nil {
			fmt.Println("Erro: ", err)
			close <- i
			return
		}

		//Separa a string no espaço
		args := strings.SplitN(message, " ", 2)
        if len(args) < 1 {
            fmt.Println("Erro: resposta fora do padrão")
            close <- i
            return
        }

        //Remove os espaços e \alguma coisa q tenham sobrado
        op := strings.TrimSpace(args[0])

		//Verifica a operação pedida pelo cliente e chama a função correta para atende-lo
		switch op {
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
            	b := decodifica([]byte(args[1]))
            	//Se for o próximo bloco esperado
            	if isBlockValid(b, Blockchain[len(Blockchain)-1]) == true {
            		client.Mutex.Lock()
            		distribuiBloco(client, b)
            		client.Mutex.Unlock()

            	} else if Blockchain[len(Blockchain)-1].Head.Index < b.Head.Index {
            		//TODO fazer solução
            		//Se a cadeia principal local tiver sido ultrapassada
            	}

            //String: CLOSE CONECTION\n
            //Caso seja o encerramento da conexão
            case "CLOSE":
            	client.Conn.Close()
            	fmt.Println("Conexão encerrada!")
            	close <- i
            	//TODO remover a conexão das abertas
            	return

            default:
            	fmt.Println("Erro: mensagem inválida")
            	return
        }

	}
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
        log.Fatal("decode error:", err)
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
	network.Write(b)
	dec := gob.NewDecoder(&network) // Will read from network.
    err := dec.Decode(&block)
    if err != nil {
        log.Fatal("decode error:", err)
    }
    fmt.Println(block.Head.Hash)
    return block
}

func imprimeBloco(b Block){
	fmt.Println("Bloco ", b.Head.Index)
	fmt.Println("Timestamp ", b.Head.Timestamp)
	fmt.Println("Table Hash ", b.Head.TableHash)
	fmt.Println("Hash ", b.Head.Hash)
	fmt.Println("Previous Hash ", b.Head.PrevHash)
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
			fmt.Println("lidos ", n)
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
	newBlock.Head.TableHash = "work on progress" //adicionar hash da tabela
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

//TODO: Deve ser mexida no futuro, envolver o valor em moedas do validador
//Calcula a dificuldade do bloco, baseando-se no coinage
func calculateDifficultyPOS(newBlock Block, oldBlock Block) (int) {
	p := fmt.Println
	t, err1 := time.Parse(TIMEFORM, newBlock.Head.Timestamp)
	tOB, err2 := time.Parse(TIMEFORM, oldBlock.Head.Timestamp)
	if err1 != nil && err2 != nil {
		p("Erro ocorrido no tempo")
	}
	
	//Faz a conta do coinage (adicionar score do validador)
	target := dCoinage - int(t.Sub(tOB).Seconds())
	//p(target)
	if target < 0 {
		target = 0
	}
	//p("A dificulade do bloco esta em: ", target)

	return target
}

//Fazer
func generatePOS() (Block){
	p := fmt.Println
	var newBlock Block
	oldBlock := Blockchain[len(Blockchain)-1]

	p("Procurando Hash válido...")
	//Tenta encontrar o hash que satisfaça a dificulade da rede
	tentativas := 1
	tempoI := time.Now()
	for {
		//TODO adicionar ID do jogador na geração dos blocos
		newBlock, _ = generateBlock(oldBlock, "")

		target := calculateDifficultyPOS(newBlock, oldBlock)

		if isHashValid(newBlock.Head.Hash, target){
			p("Hash satisfatório encontrado")
			p(newBlock.Head.Hash)
			p("Tentativas: ", tentativas)

			break
		}
		tentativas++
		time.Sleep(time.Second - 100)
	}
	p("Levou: ", int(time.Now().Sub(tempoI).Seconds()))
	return newBlock
}

func main(){
	//Trecho de código para teste de criação de blocos e o bloco Gênesis
/*
	//Criando o genesisBlock
	var genesisBlock Block
	t := time.Now()
	genesisBlock.Head.Index = 0
	genesisBlock.Head.Timestamp = t.Format(TIMEFORM)
	genesisBlock.Head.TableHash = "work in progress"
	genesisBlock.Head.PrevHash = "1"
	genesisBlock.Head.Validator = "0"
	genesisBlock.Head.Hash = calculateBlockHash(genesisBlock)
	Blockchain = []Block{genesisBlock};
	fmt.Println(Blockchain[0].Head.Hash)

	//Blockchain = carregaBlockchain()
	//Gerando novos blocos
	for i := 0; i < 10; i++ {
		//Cria o bloco usando a função criada para isso
		novo, e := generateBlock(Blockchain[(len(Blockchain)-1)], " ")
		//Verifica se não ocorreram erros no processo
		if e != nil {
			fmt.Println("FUUUUU!")
		}
		//Insere o novo bloco na nossa blockchain de teste
		Blockchain = append(Blockchain, novo)
	}

	fmt.Println(Blockchain[0].Head.Hash)

	//Testando se a blockchain continua válida
	for i := 0; i < len(Blockchain)-1; i++ {
		if isBlockValid(Blockchain[i+1], Blockchain[i]) == false {
			fmt.Println("Erouuuuu")
		}
	}

	for _, bloco := range Blockchain {
		salvaBloco(bloco)
	}
*/

	Blockchain = carregaBlockchain()
	//Teste sobre ler a tabela/codificar do csv
	data, _ := parseData("data.csv")
	t2 := codificaTabela(data)
	fmt.Println(data)
	fmt.Println(t2)
	fmt.Println(decodificaTabela(t2))
	go func() {
		j := 1
		for {
			var client Client
			client.Conn = nil
			novo := generatePOS()
			mutex.Lock()
			Blockchain = append(Blockchain, novo)
			mutex.Unlock()
			distribuiBloco(client,novo)
			fmt.Println(j)
			j++
		}
	}()
/*
	for _, bloco := range Blockchain {
		imprimeBloco(bloco)
	}
*/
/* pensar melhor sobre esse IP
	//Capturando IP
	fmt.Println("o ip deveria aparecer aqui embaixo")
	name, err := os.Hostname()
	if err != nil {
	     fmt.Printf("Oops: %v\n", err)
	     return
	}

	addrs, err := net.LookupHost(name)
	if err != nil {
	    fmt.Printf("Oops: %v\n", err)
	    return
	}

	for _, a := range addrs {
	    fmt.Println(a)
	}  

	//Capturando IP2
	ifaces, _ := net.Interfaces()
	// handle err
	for _, i := range ifaces {
	    addrs, _ := i.Addrs()
	    // handle err
	    for _, addr := range addrs {
	        var ip net.IP
	        switch v := addr.(type) {
	        case *net.IPNet:
	                ip = v.IP
	        case *net.IPAddr:
	                ip = v.IP
	        }
	        fmt.Println(ip)
	        break;
	    }
	}
*/

	//Cria um canal de saída
	exit := make(chan string)

	go server(exit)

	//Verifica se a rotina terminou e encessa
	for {
	  select {
	    case <- exit: {
	      os.Exit(0)
	    }
	  }
	}

}
