BIN := vpncheap-console
ADDR ?= 127.0.0.1:18090
CLASH ?= http://127.0.0.1:9090

# Extract just the port number for lsof checks.
ADDR_PORT := $(lastword $(subst :, ,$(ADDR)))

.PHONY: build run stop test vet clean

build:
	npm --prefix frontend run build
	go build -o $(BIN) ./cmd/vpncheap-console

run: build
	./$(BIN) -addr $(ADDR) -clash $(CLASH)

# Per ADR-0002: full shutdown — disconnect tunnel, quit VPNCheap, stop console.
# kill-by-pidfile alone is insufficient: launchd may auto-restart the process,
# or the pidfile may point to a dead PID while a different process holds the port.
# stop handles all three failure modes: launchd removal, pidfile kill, pgrep sweep.
PIDFILE := $(HOME)/.vpncheap-console.pid
stop:
	@echo "stopping vpncheap-console..."
	@# 1. Unregister from launchd (prevents auto-restart of a keepalive service).
	@launchctl remove com.vpncheap.console 2>/dev/null || true
	@launchctl bootout gui/$$(id -u)/com.vpncheap.console 2>/dev/null || true
	@# 2. Kill via pidfile if present and the PID is still alive.
	@if [ -f "$(PIDFILE)" ]; then \
		pid=$$(cat "$(PIDFILE)"); \
		if kill -0 $$pid 2>/dev/null; then \
			echo "  killing pidfile process $$pid"; \
			kill -TERM $$pid 2>/dev/null || true; \
		fi; \
	fi
	@# 3. Kill any remaining console processes by pattern (catches orphaned/restarted ones).
	@pids=$$(pgrep -f 'vpncheap-console.*-addr' 2>/dev/null | tr '\n' ' '); \
	if [ -n "$$pids" ]; then \
		echo "  killing leftover processes: $$pids"; \
		kill -TERM $$pids 2>/dev/null || true; \
	fi
	@sleep 1
	@# 4. Force kill any survivors that ignored SIGTERM.
	@pids=$$(pgrep -f 'vpncheap-console.*-addr' 2>/dev/null | tr '\n' ' '); \
	if [ -n "$$pids" ]; then \
		echo "  force killing survivors: $$pids"; \
		kill -9 $$pids 2>/dev/null || true; \
	fi
	@# 5. Clean up stale pidfile and verify the port is free.
	@if [ -f "$(PIDFILE)" ]; then rm -f "$(PIDFILE)"; fi
	@if lsof -nP -iTCP:$(ADDR_PORT) -sTCP:LISTEN >/dev/null 2>&1; then \
		echo "  WARNING: port $(ADDR_PORT) still in use"; \
	else echo "  port $(ADDR_PORT) is free"; fi
	@echo "done"

test:
	go vet ./...
	go test ./...

vet:
	go vet ./...

clean:
	rm -f $(BIN)
