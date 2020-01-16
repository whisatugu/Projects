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
//	"strconv"
	"sync"
	"time"

	"bytes"
    "encoding/gob"
    "strings"

//	"github.com/davecgh/go-spew/spew"
//	"github.com/joho/godotenv"
)

// Block represents each 'item' in the blockchain
type Block struct {
	Index     int
	Timestamp string
	BPM       int
	Hash      string
	PrevHash  string
	Validator string
}

// Blockchain is a series of validated Blocks
var Blockchain []Block
var tempBlocks []Block
var conections [3]net.Conn
var hostsIP	[]string

var tabelaJogo [10][10]string

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
	return hex.EncodeToString(hashed)
}

//calculateBlockHash returns the hash of all block information
func calculateBlockHash(block Block) string {
	record := string(block.Index) + block.Timestamp + string(block.BPM) + block.PrevHash
	return calculateHash(record)
}

// generateBlock creates a new block using previous block's hash
func generateBlock(oldBlock Block, BPM int, address string) (Block, error) {

	var newBlock Block

	t := time.Now()

	newBlock.Index = oldBlock.Index + 1
	newBlock.Timestamp = t.String()
	newBlock.BPM = BPM
	newBlock.PrevHash = oldBlock.Hash
	newBlock.Hash = calculateBlockHash(newBlock)
	newBlock.Validator = address

	return newBlock, nil
}

// isBlockValid makes sure block is valid by checking index
// and comparing the hash of the previous block
func isBlockValid(newBlock, oldBlock Block) bool {
	if oldBlock.Index+1 != newBlock.Index {
		return false
	}

	if oldBlock.Hash != newBlock.PrevHash {
		return false
	}

	if calculateBlockHash(newBlock) != newBlock.Hash {
		return false
	}

	return true
}

//Lê o arquivo csv e retorna uma matriz de strings
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

func server(exit chan string){
	//Testanto conexão server
	ln, err := net.Listen("tcp", ":8080")
	if err != nil {
		// handle erro
		fmt.Println("Error!")
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			// handle error
			fmt.Println("Error!")
		}
		go handleConnection(conn)
	}
	exit <- "sair"
}

//Recebe o index de inicio e fim solicitado
func enviaBlocos(conn net.Conn, inicio int, fim int){
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	//Caso index fim -1, envia do inicio até o bloco atual
	if fim == -1 {
		fim = Blockchain[len(Blockchain)-1].Index
	}
	//Verifica possiveis erros de index (caso a blockchain nao esteja exatamente no index do vetor,
	//isto podera ser alterado)
	if fim > Blockchain[len(Blockchain)-1].Index || inicio < Blockchain[0].Index {
		fmt.Println("Erro: Requisição de bloco forma do index")
		return
	}
	//envia os blocos
	for i := inicio; i <= fim; i++ {
		//Escreve o bloco
		rw.WriteString("a" + string(codifica(Blockchain[i])))
		rw.Flush()

		//Aguarda confirmação de recebimento
		msg, err := rw.ReadString('\n')
		if err != nil {
			// handle error
			fmt.Println("Error! ", err)
			break
		}
		fmt.Println(msg)
	}

	//Escreve a msg de finalização de operação
	_, err := rw.WriteString("b")
	if err != nil {
			// handle error
			fmt.Println("Error! ", err)
	}
	rw.Flush()
}

func enviaBlockchain2(conn net.Conn){
	//Envia blocos
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	for _, bloco := range Blockchain {
		//Escreve o bloco
		rw.WriteString("a" + string(codifica(bloco)))
		rw.Flush()

		//Aguarda confirmação de recebimento
		msg, err := rw.ReadString('\n')
		if err != nil {
			// handle error
			fmt.Println("Error! ", err)
			break
		}
		fmt.Println(msg)
	}

	//Escreve a msg de finalização de operação
	_, err := rw.WriteString("b")
	if err != nil {
			// handle error
			fmt.Println("Error! ", err)
	}
	rw.Flush()
}

//Envia toda a blockchain a um cliente
func enviaBlockchain(conn net.Conn){
	//buffer := make([]byte, 4096)
	//Envia 
	//writer := bufio.NewWriter(conn)
	//reader := bufio.NewReader(conn)
	for _, bloco := range Blockchain {
		//envia o bloco junto com o código de bloco (melhorar depois)
		fmt.Println("Enviando bloco!")
		//fmt.Fprintf(conn, "a" + string(codifica(bloco)))
		
		n, err := conn.Write([]byte("a" + string(codifica(bloco))))
		if n == 0 || err != nil {
			fmt.Println("Erouuu! ", err)
		}

		//data := codifica(bloco)
		//writer.WriteString("a" + string(data))
		//writer.Reset(writer)
		fmt.Println("Esperando confirmação")
		msg, err := bufio.NewReader(conn).ReadString('\n')
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
	n, err := conn.Write([]byte{'b'})
	if n == 0 || err != nil {
		fmt.Println("Erouuu! ", err)
	}
	*/
	fmt.Fprintf(conn, "b")
	fmt.Println("Entrega de blockchain concluída!")
}

//Mantém a conexão com o cliente e atende seus pedidos
func handleConnection(conn net.Conn) {
	r := bufio.NewReader(conn)
	for{
		//Aguarda a requisição do cliente
		message, err := r.ReadString('\n')
		if err != nil {
			fmt.Println("Erro: ", err)
		}
		//Encerra a conexão caso o cliente caia/desconecte
		if err == io.EOF {
			fmt.Println("Erro: resposta fora do padrão ou nula")
			conn.Close()
			return
		}

		//Separa a string no espaço
		args := strings.SplitN(message, " ", 2)
        if len(args) < 1 {
                fmt.Println("Erro: resposta fora do padrão")
                return
        }

        //Remove os espaços e \alguma coisa q tenham sobrado
        op := strings.TrimSpace(args[0])

		//Verifica a operação pedida pelo cliente e chama a função correta para atende-lo
		switch op {
            case "BLOCO":
            	var inicio, fim int
            	var conector string
            	//Conta o número de espaços na substring restante para saber qual a mensagem
            	n := strings.Count(args[1], " ")
                switch n {
                    //String: BLOCO ALL\n
                    //Caso em que todos os blocos são requeridos
                    case 0:
                    	enviaBlockchain2(conn)

                    //String: BLOCO SINCE 3\n
                    //Caso peça de um determinado bloco até o atual
                    case 1:
                    	fmt.Sscanf(args[1], "%d %s %d", &conector, &inicio)
                    	enviaBlocos(conn, inicio, -1)

                    //String: BLOCO 1 TO 3\n
                    //Caso onde uma fatia dos blocos são requeridos
                    case 2:
                    	fmt.Sscanf(args[1], "%d %s %d", &inicio, &conector, &fim)
                    	enviaBlocos(conn, inicio, fim)
                }

            //String: CLOSE CONECTION\n
            //Caso seja o encerramento da conexão
            case "CLOSE":
            	conn.Close()
            	fmt.Println("Conexão encerrada!")
            	//TODO remover a conexão das abertas
            	return

            default:
            	fmt.Println("Erro: mensagem inválida")
            	return
        }

	}
/*
	//Codifica o bloco e envia para o cliente, concatenando o terminador \n
	n, err := conn.Write(codifica(Blockchain[len(Blockchain)-1]))
	if n == 0 || err != nil {
		fmt.Println("Erouuu! ", err)
	}
	
	conn.Close()
	*/
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
    fmt.Println(block.Hash)
    return block
}

func imprimeBloco(b Block){
	fmt.Println("Bloco ", b.Index)
	fmt.Println("Timestamp ", b.Timestamp)
	fmt.Println("BPM ", b.BPM)
	fmt.Println("Hash ", b.Hash)
	fmt.Println("Previous Hash ", b.PrevHash)
	fmt.Println()
}

func salvaBloco(b Block) (error){
	vet := codifica(b)
	f, err := os.Create("Block_ " + strconv.Itoa(b.Index))
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

func main(){
	//Criando o genesisBlock
	var genesisBlock Block
	t := time.Now()
	genesisBlock.Index = 0
	genesisBlock.Timestamp = t.String()
	genesisBlock.BPM = 2
	genesisBlock.PrevHash = "1"
	genesisBlock.Hash = calculateBlockHash(genesisBlock)
	genesisBlock.Validator = "0"
	Blockchain = []Block{genesisBlock};
	fmt.Println(Blockchain[0].Hash)
	//Gerando novos blocos
	for i := 0; i < 10; i++ {
		//Cria o bloco usando a função criada para isso
		novo, e := generateBlock(Blockchain[(len(Blockchain)-1)], i, " ")
		//Verifica se não ocorreram erros no processo
		if e != nil {
			fmt.Println("FUUUUU!")
		}
		//Insere o novo bloco na nossa blockchain de teste
		Blockchain = append(Blockchain, novo)
		salvaBloco(novo)
	}

	fmt.Println(Blockchain[0].Hash)

	data, _ := parseData("data.csv")

	//Testando se a blockchain continua válida
	for i := 0; i < len(Blockchain)-1; i++ {
		if isBlockValid(Blockchain[i+1], Blockchain[i]) == false {
			fmt.Println("Erouuuuu")
		}
	}

	t2 := codificaTabela(data)

	fmt.Println(data)
	fmt.Println(t2)
	fmt.Println(decodificaTabela(t2))

/*
	for _, bloco := range Blockchain {
		imprimeBloco(bloco)
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
