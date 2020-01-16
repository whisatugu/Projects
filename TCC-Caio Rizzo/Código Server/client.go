package main

import (
	"bufio"
//	"crypto/sha256"
//	"encoding/hex"
//	"encoding/json"
	"fmt"
	"io"
	"log"
//	"math/rand"
	"net"
//	"os"
//	"strconv"
//	"sync"
//	"time"

	"bytes"
    "encoding/gob"
)

type Block struct {
	Index     int
	Timestamp string
	BPM       int
	Hash      string
	PrevHash  string
	Validator string
}

var Blockchain []Block

func solicitaBlockchain(conn net.Conn){
	buffer := make([]byte, 4096)
	reader := bufio.NewReader(conn)

	//Envia o comando de solicitar todos os blocos ao servidor
	n, err := conn.Write([]byte("BLOCO ALL\n"))
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
		fmt.Println(buffer)
		if err != nil && err != io.EOF && n > 0 {
			// handle error
			fmt.Println("Error! ", err)
			break
		}

		//verifica erros (trocar por um switch case)
		if string(buffer)[0] != 'a' && string(buffer)[0] != 'b'{
			fmt.Println("Error no envio")
			break
		}
		if string(buffer)[0] == 'b'{
			fmt.Println("Blockchain sucessfull received! ")
			break
		}
		
		//Decodifica o bloco e coloca na blockchain
		Blockchain = append(Blockchain, decodifica(buffer[1:n]))
		fmt.Fprintf(conn, "OK\n")
	}
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
    imprimeBloco(block)
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

func main(){
	//Recebe tipo de conexão e endereço do server com a porta
	conn, err := net.Dial("tcp", "192.168.1.19:8080")
	defer conn.Close()
	if err != nil {
		// handle error
		fmt.Println("Error!")
		return
	}

	//Escreve na conn criada

	/*
	//fmt.Fprintf(conn, "GET / HTTP/1.0\r\n\r\n")
	n, err := conn.Write([]byte("GET / HTTP/1.0\r\n\r\n"))
	if n == 0 || err != nil {
		fmt.Println("Erouuu! ", err)
	}

	*/
	solicitaBlockchain(conn)
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