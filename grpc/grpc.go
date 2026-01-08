package main

import (
	"flag"
	"fmt"
)

func main() {
	// read CLI arguments
	sPtr := flag.String("s", "", "server URL")
	pPtr := flag.Int("p", 9876, "port number")
	masterPtr := flag.String("m", "", "URL of master/parent server for s2s verižna replikacija")
	flag.Parse()

	url := fmt.Sprintf("%v:%v", *sPtr, *pPtr)

	// start server or client
	if *sPtr == "" {
		fmt.Println("Starting gRPC server on", url)
		Server(url, masterPtr)
	} else {
		fmt.Println("Starting gRPC client, connecting to", url)
		ClientMain(url)
	}
}
