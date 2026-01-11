package main

import (
	"flag"
	"log"
)

func main() {
	// read CLI arguments
	connectUrl := flag.String("r", "", "connect to server on URL, for example localhost:9875")
	listenUrl := flag.String("l", "[::]:9875", "listen with server on URL")
	myUrl := flag.String("m", "", "my server URL, npr. hostname:9875")
	dbfilePtr := flag.String("d", ":memory:", "sqlite3 db file name")
	controlplanePtr := flag.String("c", "", "URL of control plane")
	s2spskPtr := flag.String("g", "skrivnost", "s2s psk za verižno replikacijo, something random and static")
	jwtSecretPtr := flag.String("j", "jwt secret this is insecure", "jwt secret, should be changed to something random and static")
	// uName := flag.String("u", "Bauer", "username")
	flag.Parse()

	jwtSecret = []byte(*jwtSecretPtr)

	// start server or client
	if *connectUrl == "" {
		if *controlplanePtr != "" && *myUrl == "" {
			log.Fatal("nastavi myUrl, če uporabljaš contorlplane")
		}
		log.Println("Starting gRPC server on", *listenUrl)
		Server(*listenUrl, *controlplanePtr, *s2spskPtr, *myUrl, *dbfilePtr)
	} else {
		// log.Println("Starting gRPC client, connecting to", *connectUrl)
		// ClientMain(*connectUrl)
		fmt.Println("Starting gRPC TUI client, connecting to", *connectUrl)
		StartTUI(*connectUrl)
	}

}
