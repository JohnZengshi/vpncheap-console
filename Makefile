BIN := vpncheap-console
ADDR ?= 127.0.0.1:18090
CLASH ?= http://127.0.0.1:9090

.PHONY: build run stop test vet clean

build:
	go build -o $(BIN) ./cmd/vpncheap-console

run: build
	./$(BIN) -addr $(ADDR) -clash $(CLASH)

# Per ADR-0002: full shutdown — disconnect tunnel, quit VPNCheap, stop console.
PIDFILE := $(HOME)/.vpncheap-console.pid
stop:
	@if [ -f "$(PIDFILE)" ]; then \
		pid=$$(cat "$(PIDFILE)"); \
		echo "stopping console (pid $$pid), disconnecting tunnel, quitting VPNCheap"; \
		kill -TERM $$pid; \
		sleep 1; \
		rm -f "$(PIDFILE)"; \
	else echo "no pidfile at $(PIDFILE), console not running?"; fi

test:
	go vet ./...
	go test ./...

vet:
	go vet ./...

clean:
	rm -f $(BIN)
