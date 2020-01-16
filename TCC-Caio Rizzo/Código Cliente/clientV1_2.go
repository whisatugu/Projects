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
//	"time""
	
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
	record := string(block.Head.Index) + block.Head.Timestamp + block.Head.TableHash + block.Head.PrevHash
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

//Lê do arquivo contendo servidores conhecidos e o ID cadastrado neles
//Caso
func openConn(conn net.Conn){
	//buffer := make([]byte, 4096)
	reader := bufio.NewReader(conn)

	//Envia o comando de solicitar todos os blocos ao servidor
	n, err := conn.Write([]byte("OPEN 0\n"))
	if n == 0 || err != nil {
		fmt.Println("Error: lendo da conexão", err)
		return
	}
	fmt.Println("Esperando resposta do servidor...")
	//Recebe o ID ou uma mensagem de confirmação OK/ERRO
	msg, err := reader.ReadString('\n')
	msg = strings.TrimSpace(msg)
	fmt.Println(msg)
	if err != nil {
    	fmt.Println("Error: abrindo arquivo")
    	return
    }
    if msg == "OK" {
    	fmt.Println("Validado no servidor com sucesso!")

    } else if msg == "ERROR" {
    	//DO SOMETHING
    	//Procura outro servidor na lista ou algo do tipo

    } else {
		//Abre arquivo
		f, err := os.OpenFile("servers.csv", os.O_APPEND|os.O_WRONLY, 0600)
	    if err != nil {
	    	fmt.Println("Error: abrindo arquivo")
	    	return
	    }
	    fmt.Println(msg, conn.RemoteAddr().String())
	    fmt.Fprintf(f, "%s,%s", msg, conn.RemoteAddr().String())
	    defer f.Close()
	}
    return
}

//TODO testar função
//Recupera a cadeia principal do servidor em caso onde estão incompatíveis
//O inteiro i representa a partir de qual index onde serão pedidos os blocos
func recuperaCadeiaPrincipal(conn net.Conn, i int) {
	msg := make([]byte, 4096)
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	//Blockchain temporária para realizar possíveis row backs
	var BlockchainTemp []Block
	var bloco Block
	//Requisita blocos até encontrar a ligação com a blockchain local
	for ;i > 0 && len(Blockchain) <= i; i-- {
		rw.WriteString("BLOCK BEFORE " + strconv.Itoa(i) + "\n")
		rw.Flush()
		msg, err := rw.ReadString('\n')
		if err != nil {
			fmt.Println("Erro: resposta fora do padrão, erro não tratado ou nula\n", err)
			break
		}
		//Decodifica blcoo
		bloco = decodifica(msg)
		
		//Se verdade, encontramos de volta o elo de ligação com a cadeia existente local
		if isBlockValid(bloco, Blockchain[i-1]) == true {
			break

		} else {
			//Salva o último bloco requerido na blockchain temporaria
			BlockchainTemp = append(BlockchainTemp, bloco)
		}
	}

	//Após terminar o for, ou o bloco de ligação foi encontrado ou toda blockchain estava errada (pior caso)
	
	//Caso especial onde é encontrado de cara
	if len(Blockchain) == i {
		Blockchain = append(Blockchain, bloco)

	} else if i > 0 {
		//Retira da blockchain temporaria e coloca na local
		for j := 0; i < len(Blockchain) && j < len(BlockchainTemp); j++ {
			if isBlockValid(BlockchainTemp[j], Blockchain[i-1]) == true {
				Blockchain[i] = BlockchainTemp[j]
				i++
			} else {
				fmt.Println("ERRO blockchain temporaria inconsistente")
				return
			}
		}
	} else {
		//Neste caso mantém a blockchain local, pois algo errado ocorreu
		fmt.Println("Error: algo muito errado esta acontecendo! (possível ataque)")
	}
}

//TODO testar função
//Erros: Não está recebendo todos os bytes do bloco, corrigir!
//Faz a requisição de novos blocos ao servidor conectado
//Chamada sempre que um servidor inicializa para fazer a atualização da blockchain
func atualizaBlockchain(conn net.Conn){
	msg := make([]byte, 4096)
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	// Requisita do bloco de maior indice local até o mais recente validado no servidor
	if len(Blockchain) > 0 {
		inicio := Blockchain[len(Blockchain)-1].Head.Index
		fmt.Println("Sera pedido a partir de ", inicio)
		rw.WriteString("BLOCK SINCE " + strconv.Itoa(inicio) + "\n")
		rw.Flush()

		//Recebe os blocos, valida e os adicina na blockchain local
		for {
			n, err := rw.Read(msg)
			if err != nil {
				fmt.Println("Erro: resposta fora do padrão, erro não tratado ou nula\n", err)
				break
			}
			//msg = strings.TrimSpace(msg)
			//Verifica se o envio já foi concluído
			if string(msg)[:3] == "END" {
				fmt.Println("Recebimento de blocos realizado com sucesso!")
				break
			}
			//Caso não, valida e adiciona o bloco
			bloco := decodifica(msg) //Provavelmente dara errado
			imprimeBloco(bloco)
			if isBlockValid(bloco, Blockchain[len(Blockchain)-1]) == true {
				Blockchain = append(Blockchain, bloco)
				rw.WriteString("OK\n")
				rw.Flush()
			} else {
				//OBS: O único momento em que este else deve ocorrer é na primeira iteração do for

				//Trata erro de blockchain incompatível com a versão do servidor
				//Envia um erro para sinalizar o problema e encerra esta requisição
				//TODO: Fazer mapeamento correto de códigos de erro
				rw.WriteString("ERROR 1\n")
				rw.Flush()
				
				//TODO usar a função criada aqui
				//Faz a requisição de blocos anteriores até obter consistencia com o servidor

			}
		}
	}
}

func solicitaBlockchain(conn net.Conn){
	buffer := make([]byte, 4096)
	reader := bufio.NewReader(conn)

	//Envia o comando de solicitar todos os blocos ao servidor
	n, err := conn.Write([]byte("BLOCK ALL\n"))
	if n == 0 || err != nil {
		fmt.Println("Erouuu! ", err)
	}

	for {
		/*
		//le o primeiro byte para saber se é um bloco
		msg, err := bufio.NewReader(conn).ReadByte()
		if err != nil && err != io.EOF {
				// handle error
				fmt.Println("Error! ", err)
		}

		//verifica erros (trocar por um switch case)
		if string(msg)[0] != 'a' && string(msg)[0] != 'b'{
			fmt.Println("Error no envio")
			break
		}
		if string(msg)[0] == 'b'{
			fmt.Println("Blockchain sucessfull received! ")
			break
		}
		*/

		//Le o total enviado
		fmt.Println("Esperando próximo bloco ")
		n, err := reader.Read(buffer)
		//fmt.Println(buffer, n)
		if err != nil && err != io.EOF && n > 0 {
			// handle error
			fmt.Println("Error! ", err)
			break
		}

		//verifica erros (trocar por um switch case)
		//if string(buffer)[0] != 'a' && string(buffer)[0] != 'b'{
		//	fmt.Println("Error no envio")
		//	break
		//}

		//Verifica se é a mensagem de confirmação de fim de operação
		//Melhor usar um bufio readString
		if buffer[0] == 'E' && buffer[1] == 'N' && buffer[2] == 'D' && buffer[3] == '\n'{
			fmt.Println("Blockchain sucessfull received! ")
			break
		}
		
		//Decodifica o bloco e coloca na blockchain
		Blockchain = append(Blockchain, decodifica(buffer[:n]))
		fmt.Fprintf(conn, "OK\n")
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
	f, err := os.Create("Block_ " + strconv.Itoa(b.Head.Index))
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
		f, err := os.Open("Block_ " + strconv.Itoa(i))
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