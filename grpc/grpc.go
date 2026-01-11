package main
import (
	"flag"
	"log"
	"fmt"
)
func main () {
	// read CLI arguments
	connectUrl := flag.String("r", "", "connect to control plane on URL, for example localhost:9875")
	listenUrl := flag.String("l", "[::]:9875", "listen with server on URL")
	myUrl := flag.String("m", "", "my server URL, npr. hostname:9875")
	dbfilePtr := flag.String("d", ":memory:", "sqlite3 db file name")
	s2spskPtr := flag.String("g", "skrivnost", "s2s psk za verižno replikacijo, something random and static")
	jwtSecretPtr := flag.String("j", "jwt secret this is insecure", "jwt secret, should be changed to something random and static")
	legacyPtr := flag.String("t", "", "Use legacy ClientMain for testing client with given username")
	// uName := flag.String("u", "Bauer", "username")
	flag.Parse()
	jwtSecret = []byte(*jwtSecretPtr)
	if *myUrl != "" {
		log.Println("Starting gRPC server on", *listenUrl)
		Server(*listenUrl, *connectUrl, *s2spskPtr, *myUrl, *dbfilePtr)
	} else {
		if *legacyPtr != "" {
			log.Println("Starting legacy textual testing gRPC client, connecting to", *connectUrl)
			ClientMain(*connectUrl, *legacyPtr)
		} else {
			fmt.Println("Starting gRPC TUI client, connecting to", *connectUrl)
			StartTUI(*connectUrl)
		}
	}
}
