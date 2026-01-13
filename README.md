# klepetalnica - ps projekt
Rok za oddajo: 12. 1. 2026 23:59

Poženi nadzorno ravnino ali dve ali tri (če imaš samo eno, ne nastavi argumentov bootstrap, raftbind, raftaddress in cluster): TOLE POČNI V MAPI grpc/controlplane
```
go run controlplanemain.go controlplaneimpl.go odjemalec.go --bind [::]:9800 --raftbind [::]:9810 --raftaddress localhost:9810 --bootstrap --myurl localhost:9800
go run controlplanemain.go controlplaneimpl.go odjemalec.go --bind [::]:9801 --raftbind [::]:9811 --raftaddress localhost:9811 --cluster localhost:9800 --myurl localhost:9801
go run controlplanemain.go controlplaneimpl.go odjemalec.go --bind [::]:9802 --raftbind [::]:9812 --raftaddress localhost:9812 --cluster localhost:9800 --myurl localhost:9802
```

Poženi strežnik ali dva ali tri:
```
go run grpc.go streznik.go odjemalec.go tui.go -l [::]:9820 -r localhost:9800 -m localhost:9820 -d 0.db
go run grpc.go streznik.go odjemalec.go tui.go -l [::]:9821 -r localhost:9800 -m localhost:9821 -d 1.db
go run grpc.go streznik.go odjemalec.go tui.go -l [::]:9822 -r localhost:9800 -m localhost:9822 -d 2.db
```

Poženi odjemalca ali dva ali tri in podaj naslov controlplanea:
```
go run grpc.go streznik.go odjemalec.go tui.go -r localhost:9800
```

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

glej mapo entr za ukaze za iteracijo z entr(1) POZOR, TO SO STARI UKAZI ZA NEK STAR COMMIT, NE DELAJO NA NOVI VERZIJI

Prevedi protoc:
```
cd protobuf; protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative razpravljalnica.proto
```

V okoljske spremenljivke lahko daš
```
export GRPC_GO_LOG_VERBOSITY_LEVEL=99
export GRPC_GO_LOG_SEVERITY_LEVEL=info
```
za več logov

za poganjanje integration testov moraš imeti nadzorno ravnino na localhost:9800 in nato v mapi grpc poženi
```
cd odjemalec; go test -tags=integration -v
```
