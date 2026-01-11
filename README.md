# klepetalnica - ps projekt
Rok za oddajo: 12. 1. 2026 23:59

ukaze poganjaj v podmapi grpc

Poženi strežnik:
```
go run grpc.go streznik.go odjemalec.go tui.go -l [::]:9875 -r localhost:9870 -m localhost:9875 -d database.db
```

Poženi odjemalca:
```
go run grpc.go streznik.go odjemalec.go tui.go -r localhost:9875 2> /dev/null
```

Prevedi protoc:
```
cd protobufRazpravljalnica; protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative razpravljalnica.proto
```

Poženi nadzorno ravnino:
```
go run controlplanemain.go controlplaneimpl.go odjemalec.go
```

V okoljske spremenljivke lahko daš
```
export GRPC_GO_LOG_VERBOSITY_LEVEL=99
export GRPC_GO_LOG_SEVERITY_LEVEL=info
```
za več logov


## kako uporabljati TUI

1. Prijavi se z nekim že obstoječim UserID (nizke številke)

2. Po Topicih se sprehajaš s tipkami gor/dol ali s tab

3. Topic izbereš s tipko enter

4. Napišeš Message in ga pošlješ s tipko enter

5. Spreminjanje topica - tipka Esc

6. Všečkanje sporočil (ko si v nekem topicu) - s Ctrl+L prideš v okvirček, kjer so sporočila, nato pa se s tab/puščicami premikaš po sporočilih. Pritisni Enter, da jih všečkaš

7.1 načeloma Ctrl+M da greš nazaj na pisanje, ampak trenutno ne dela

7.2 Pisanje lahko nadaljuješ tako da klikneš Esc, in ponovno izbereš želeni topic

8. Program zapreš z Ctrl+C

development
===========

glej mapo entr za ukaze za iteracijo z entr(1)
