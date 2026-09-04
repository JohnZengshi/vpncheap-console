package main

import (
	"bufio"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"fmt"
	"testing"
)

// TestProxyHandlerCONNECTSwitchesNode tests that a CONNECT request triggers
// the selector switch before dialing the target.
func TestProxyHandlerCONNECTSwitchesNode(t *testing.T) {
	clash, puts := fakeClashAPI(t, []string{"node-a", "node-b"})
	defer clash.Close()

	// fake upstream TCP server (the "target" the CONNECT dials)
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	go func() {
		conn, _ := upstream.Accept()
		if conn != nil {
			conn.Write([]byte("hello-from-target"))
			conn.Close()
		}
	}()

	cfg := proxyConfig{
		clashBase: clash.URL,
		rules:     map[string]string{"ipinfo.io": "node-b"},
		fallback:  "node-a",
	}
	handler := newProxyHandler(newProxyManager(cfg, true, "127.0.0.1:2323"))

	// We need a real TCP listener to test CONNECT (it uses Hijack).
	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer proxyListener.Close()
	srv := &http.Server{Handler: handler}
	go srv.Serve(proxyListener)

	// Send a CONNECT request
	targetAddr := upstream.Addr().String()
	// extract domain for the test: use a fake host that maps to node-b
	// We can't really override the CONNECT host, so we set rules for "127.0.0.1"
	// Actually, CONNECT target is "host:port". We need the domain to match a rule.
	// Use a domain that resolves to 127.0.0.1 — but tests can't rely on DNS.
	// Instead, test that CONNECT with a mapped domain triggers PUT.
	// We'll connect to the real upstream via IP but set the Host to ipinfo.io.
	conn, err := net.Dial("tcp", proxyListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Send CONNECT ipinfo.io:443 (the handler will try to dial ipinfo.io:443,
	// which won't connect in test env, but we test that PUT happened first)
	// Actually we need the dial to succeed for the tunnel. Let's use the upstream.
	// CONNECT <upstream-addr> but map "127.0.0.1" → node-b
	cfg.rules = map[string]string{"127.0.0.1": "node-b"}
	handler = newProxyHandler(newProxyManager(cfg, true, "127.0.0.1:2323"))
	srv2 := &http.Server{Handler: handler}
	proxyListener2, _ := net.Listen("tcp", "127.0.0.1:0")
	defer proxyListener2.Close()
	go srv2.Serve(proxyListener2)

	conn2, err := net.Dial("tcp", proxyListener2.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close()
	fmt.Fprintf(conn2, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", targetAddr, targetAddr)

	// Read response — expect 200 Connection Established
	reader := bufio.NewReader(conn2)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if !strings.Contains(statusLine, "200") {
		t.Fatalf("expected 200, got %q", statusLine)
	}

	// Assert PUT was called with node-b (mapped from "127.0.0.1" domain)
	got := puts.Load().([]string)
	if len(got) == 0 {
		t.Fatal("expected PUT to clash API, got none")
	}
	if got[0] != "node-b" {
		t.Fatalf("expected PUT to node-b, got %q", got[0])
	}
}

// TestProxyHandlerCONNECTSwitchFailure tests that CONNECT returns 502
// when the selector switch fails.
func TestProxyHandlerCONNECTSwitchFailure(t *testing.T) {
	clash := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/version" {
			w.Write([]byte("{}"))
			return
		}
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer clash.Close()

	cfg := proxyConfig{
		clashBase: clash.URL,
		rules:     map[string]string{"127.0.0.1": "node-b"},
		fallback:  "node-a",
	}
	handler := newProxyHandler(newProxyManager(cfg, true, "127.0.0.1:2323"))

	proxyListener, _ := net.Listen("tcp", "127.0.0.1:0")
	defer proxyListener.Close()
	srv := &http.Server{Handler: handler}
	go srv.Serve(proxyListener)

	conn, _ := net.Dial("tcp", proxyListener.Addr().String())
	defer conn.Close()
	fmt.Fprintf(conn, "CONNECT 127.0.0.1:9999 HTTP/1.1\r\nHost: 127.0.0.1:9999\r\n\r\n")

	reader := bufio.NewReader(conn)
	statusLine, _ := reader.ReadString('\n')
	if !strings.Contains(statusLine, "502") {
		t.Fatalf("expected 502 on switch failure, got %q", statusLine)
	}
}

// compile-time: ensure we use json
var _ = json.Marshal
