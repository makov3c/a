# klepetalnica - ps projekt
Rok za oddajo: 12. 1. 2026


Pojdi v mapo grpc

Poženi strežnik:
```
go run . -p 9876
```

Poženi odjemalca:
```
go run . -s localhost
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
