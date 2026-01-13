ALLSOURCES = $(wildcard protobuf/*.o) $(wildcard odjemalec/*.o) $(wildcard cmd/razpravljalnica/*.go) $(wildcard/cmd/controlplane/*.go)

all: razpravljalnica controlplane

razpravljalnica: $(ALLSOURCES)
	go build ./cmd/$@

controlplane: $(ALLSOURCES)
	go build ./cmd/$@

clean:
	rm razpravljalnica controlplane test

.PHONY: clean
