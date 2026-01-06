# klepetalnica - ps projekt
Rok za oddajo: 12. 1. 2026

Poženi strežnik:
```
go run grpc.go streznik.go odjemalec.go -p 9875
```

Poženi odjemalca:
```
go run grpc.go streznik.go odjemalec.go -s localhost -p 9875
```
