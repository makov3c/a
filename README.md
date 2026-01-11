# klepetalnica - ps projekt
Rok za oddajo: 11. 1. 2026 23:59

ukaze poganjaj v podmapi grpc

Poženi strežnik (če si brez nadzorne ravnine odstrani -c xxx in -m xxx):
```
go run grpc.go streznik.go odjemalec.go -l [::]:9875 -c localhost:9870 -m localhost:9875
```

Poženi odjemalca:
```
go run grpc.go streznik.go odjemalec.go -r localhost:9875
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
