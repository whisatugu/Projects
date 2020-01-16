package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
//	"encoding/json"
	"fmt"
	"io"
	"log"
//	"math/rand"
	"net"
	"os"
	"encoding/csv"
	"strings"
	"strconv"
//	"sync"
	"time"
	"errors"
	
	"bytes"
    "encoding/gob"
)

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

const TIMEFORM = "2006-01-02 15:04:05.999999999 -0700 MST"
var TIMEOUT = 5 * time.Second

var Blockchain []Block

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
	//record := string(block.Head.Index) + block.Head.Timestamp + block.Head.TableHash + block.Head.PrevHash
	t, _ := time.Parse(TIMEFORM, block.Head.Timestamp)
	record := string(block.Head.Index) + block.Head.TableHash + block.Head.PrevHash + block.Head.Validator + string(t.Second())
	return calculateHash(record)
}

//Verifica a igualdade entre dois blocos
func isBlockEqual(newBlock, oldBlock Block) bool {
	if oldBlock.Head.Index != newBlock.Head.Index {
		return false
	}

	if oldBlock.Head.Hash != newBlock.Head.Hash {
		return false
	}

	return true
}

// isBlockValid makes sure block is valid by checking index
// and comparing the hash of the previous block
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

//Lê do arquivo contendo servidores conhecidos e o ID cadastrado neles
//Caso
func openConn(conn net.Conn){
	var msg string
	var err error
	//buffer := make([]byte, 4096)
	reader := bufio.NewReader(conn)
	c1 := make(chan string, 1)

	//Envia o comando de solicitar todos os blocos ao servidor
	n, err := conn.Write([]byte("OPEN 0\n"))
	if n == 0 || err != nil {
		fmt.Println("Error: lendo da conexão", err)
		return
	}
	fmt.Println("Esperando resposta do servidor...")
	
	go func() {
		//Recebe o ID ou uma mensagem de confirmação OK/ERRO
		msg, err = reader.ReadString('\n')
    	c1 <- msg
	}()
	if err != nil {
	    fmt.Println("Error: abrindo arquivo")
	    return
    }
	//Espera o retorno no tempo, caso contrário ocorre o timeout
	select {
	case res := <-c1:
		args := strings.SplitN(res, " ", 2)
		op := strings.TrimSpace(args[0])
		fmt.Println(op)
		if op == "OK" {
			fmt.Println("Validado no servidor com sucesso!")
		} else if op == "ERRO" {
			//TODO Verificar isso
			fmt.Println("Erro: erro de número ", args[1])
			return
		} else {
		//Abre arquivo
		f, err := os.OpenFile("servers.csv", os.O_APPEND|os.O_WRONLY, 0600)
	    if err != nil {
	    	fmt.Println("Error: abrindo arquivo")
	    	return
	    }
	    fmt.Println(msg, conn.RemoteAddr().String())
	    fmt.Fprintf(f, "%s,%s\n", msg, conn.RemoteAddr().String())
	    defer f.Close()
		}
	case <-time.After(TIMEOUT):
    	fmt.Println("Erro: Connection timed out!")
    	return
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
	fmt.Println("Entrando na função de recuperar cadeia...")
	msg := make([]byte, 4096)
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	//Blockchain temporária para realizar possíveis row backs
	var BlockchainTemp []Block
	var bloco Block
	//Requisita blocos até encontrar a ligação com a blockchain local
	for ;i > 0 && i <= len(Blockchain); i-- {
		fmt.Println("For")
		rw.WriteString("BLOCK BEFORE " + strconv.Itoa(i) + "\n")
		rw.Flush()

		go func() {
			_, err = rw.Read(msg)
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
			fmt.Println(op)
			if op == "OK" {
				//Continua executando o próximo bloco
				continue
			} else if op == "ERRO" {
				//TODO Verificar isso
				fmt.Println("Erro: erro de número ", args[1])
				return errors.New("erro de número " + args[1])
			}
			fmt.Println(res)
    	case <-time.After(TIMEOUT):
        	fmt.Println("Erro: Connection timed out!")
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
			fmt.Println("For com j = ", j)
			imprimeBloco(BlockchainTemp[j])
			if isBlockValid(BlockchainTemp[j], Blockchain[i-1]) == true {
				Blockchain[i] = BlockchainTemp[j]
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
					fmt.Println("Entrou no END")
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

	for {
		//Le o total enviado
		fmt.Println("Esperando próximo bloco ")

		go func() {
			_, err = rw.Read(buffer)
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
			fmt.Println(op)
			if op == "END" {
				//Verifica se é a mensagem de confirmação de fim de operação e retorna
				fmt.Println("Blockchain sucessfull received! ")
				return
			} else if op == "ERRO" {
				//TODO Verificar isso
				fmt.Println("Erro: erro de número ", args[1])
				return
			} else {
				//Decodifica o bloco e coloca na blockchain
				Blockchain = append(Blockchain, decodifica(buffer))
				imprimeBloco(decodifica(buffer))
				rw.WriteString("OK\n")
				rw.Flush()
			}
    	case <-time.After(TIMEOUT):
        	fmt.Println("Erro: Connection timed out!")
        	return
    	}
	}
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
    //imprimeBloco(block)
    return block
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

func imprimeBloco(b Block){
	fmt.Println("Bloco ", b.Head.Index)
	fmt.Println("Timestamp ", b.Head.Timestamp)
	fmt.Println("Table Hash ", b.Head.TableHash)
	fmt.Println("Hash ", b.Head.Hash)
	fmt.Println("Previous Hash ", b.Head.PrevHash)
	fmt.Println()
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

//Mantém a conexão com o server e atende seus pedidos
func handleServerMensages(conn net.Conn, close chan int, i int) (error) {
	r := bufio.NewReader(conn)
	for{
		fmt.Println("Aguardando requisição de cliente...")
		//Aguarda a requisição do cliente
		message, err := r.ReadString('\n')
		if err != nil {
			fmt.Println("Erro: ", err)
			return err
		}
		//Encerra a conexão caso o cliente caia/desconecte
		if err == io.EOF {
			fmt.Println("Erro: resposta fora do padrão ou nula")
			conn.Close()
			return err
		}

		//Separa a string no espaço
		args := strings.SplitN(message, " ", 2)
        if len(args) < 1 {
            fmt.Println("Erro: resposta fora do padrão")
            return errors.New("resposta fora do padrão")
        }

        //Remove os espaços e \alguma coisa q tenham sobrado
        op := strings.TrimSpace(args[0])

		//Verifica a operação pedida pelo cliente e chama a função correta para atende-lo
		switch op {
			case "WIN":
				fmt.Println("Recebido novo Bloco forjado")
            	b := decodifica([]byte(args[1]))

            	//TESTE
            	//Se for o próximo bloco esperado
            	if isBlockValid(b, Blockchain[len(Blockchain)-1]) == true {
            		//distribuiBloco(conn, b)
            		Blockchain = append(Blockchain, b)
					salvaBloco(b)
					imprimeBloco(b)
					//fmt.Fprintf(conn, "OK\n")

				//Se a cadeia principal local tiver sido ultrapassada
            	} else if Blockchain[len(Blockchain)-1].Head.Index < b.Head.Index {
            		atualizaBlockchain(conn)
            	} else {
            		fmt.Println("Bloco recebido menor que a cadeia existente")
            	}

            //String: CLOSE CONECTION\n
            //Caso seja o encerramento da conexão
            case "CLOSE":
            	conn.Close()
            	fmt.Println("Conexão encerrada!")
            	close <- i
            	//TODO remover a conexão das abertas
            	return nil

            default:
            	fmt.Println("Erro: mensagem inválida")
            	return errors.New("mensagem inválida")
        }

	}
}

func main(){
	Blockchain = nil

	//Carrega blockchain local na memória
	Blockchain = carregaBlockchain()

	//Lê arquivo de servers para a memoria
	data, err := parseData("servers.csv")
	if err != nil || len(data) < 2 {
		fmt.Println("Erro: Abertura de arquivo de servers")
		return
	}

	//TODO: Escolher servidores aleatóriamente na tabela

	//Recupera um IP de servidor da tabela e tenta conexão
	//Separa o ip no :
	ip := strings.SplitN(data[1][1], ":", 2)
	if len(ip) < 1 {
        fmt.Println("Erro: IP lido fora do padrão")
        return
    }

    fmt.Println(ip[0])
	
	conn, err := net.Dial("tcp", ip[0] + ":8080")//net.Dial("tcp", "192.168.0.35:8080")
	defer conn.Close()
	if err != nil {
		// handle error
		fmt.Println("Error!")
		return
	}
	fmt.Println("Conected")

	//Escreve na conn criada

	/*
	//fmt.Fprintf(conn, "GET / HTTP/1.0\r\n\r\n")
	n, err := conn.Write([]byte("GET / HTTP/1.0\r\n\r\n"))
	if n == 0 || err != nil {
		fmt.Println("Erouuu! ", err)
	}
	*/


	//Se não foi possível, solicita a um servidor
	if Blockchain == nil{
		fmt.Println("Blockchain local não encontrada!\nBaixando de um servidor...")
		solicitaBlockchain(conn)
		for _, bloco := range Blockchain{
			salvaBloco(bloco)
		}
	} else {
		//Se não atualiza a blockchain local
		atualizaBlockchain(conn)
		fmt.Println("Blockchain Atualizada!")
	}

	close := make(chan int)

	for{
		if handleServerMensages(conn, close, 0) != nil {
			break
		}
	}

	var i int
	for{
		fmt.Scanf("%d", &i)
		if i == 1{
			openConn(conn)
			fmt.Println("Fim de Operação")
		}
		if i == 2{
			solicitaBlockchain(conn)
			fmt.Println("Fim de Operação")
		}
	}


/*
	//Recebe o bloco e imprime
	//n = bufio.NewReader(conn).Size()
	//fmt.Println(n)
	buffer := make([]byte, 4096)
	//bufio.NewScanner(conn).Buffer(buffer, n)
	//buffer = bufio.NewScanner(conn).Bytes()


	n, err = conn.Read(buffer)
	
	buffer, err := bufio.NewReader(conn).ReadBytes('\n')
	
	if err != nil && err != io.EOF {
		// handle error
		fmt.Println("Error! ", err)
		return
	}
	
	fmt.Println(n)
	bloco := decodifica(buffer)
	imprimeBloco(bloco)
	Blockchain = append(Blockchain, bloco)
	*/

	n, err := conn.Write([]byte("CLOSE CONECTION\n"))
	if n == 0 || err != nil {
		fmt.Println("Erouuu! ", err)
	}

	fmt.Println("Agora exibindo a blockchain recebida: ")
	for _, bloco := range Blockchain {
		imprimeBloco(bloco)
	}
	fmt.Println(len(Blockchain))
}