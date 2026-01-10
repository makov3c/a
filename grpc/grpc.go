package main

import (
	"flag"
	"fmt"
)

func main() {
	// read CLI arguments
	sPtr := flag.String("s", "", "server URL")
	pPtr := flag.Int("p", 9876, "port number")
	// uName := flag.String("u", "Bauer", "username")

	flag.Parse()

	url := fmt.Sprintf("%v:%v", *sPtr, *pPtr)

	// start server or client
	if *sPtr == "" {
		fmt.Println("Starting gRPC server on", url)
		Server(url)
	} else {
		fmt.Println("Starting gRPC TUI client, connecting to", url)
		StartTUI(url)
	}

}
