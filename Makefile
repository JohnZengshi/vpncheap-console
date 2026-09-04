BIN := vpncheap-console
ADDR ?= 127.0.0.1:18090
CLASH ?= http://127.0.0.1:9090

.PHONY: build run test vet clean

build:
	go build -o $(BIN) ./cmd/vpncheap-console

run: build
	./$(BIN) -addr $(ADDR) -clash $(CLASH)

test:
	go vet ./...
	go test ./...

vet:
	go vet ./...

clean:
	rm -f $(BIN)
