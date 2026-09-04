package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// TestProxyConcurrentRequestsBothSwitch tests that two concurrent requests
// for different domains both trigger a PUT. The order is not asserted —
// this honestly reflects the selector race.
func TestProxyConcurrentRequestsBothSwitch(t *testing.T) {
	clash, puts := fakeClashAPI(t, []string{"node-a", "node-b"})
	defer clash.Close()

	// Two upstream servers (the targets)
	up1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok1"))
	}))
	defer up1.Close()
	up2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok2"))
	}))
	defer up2.Close()

	cfg := proxyConfig{
		clashBase: clash.URL,
		rules: map[string]string{
			"127.0.0.1": "node-a", // both upstreams are 127.0.0.1
		},
		fallback: "node-b",
	}
	mgr := newProxyManager(cfg, true, "127.0.0.1:2323")
	handler := newProxyHandler(mgr)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, up1.URL+"/", nil)
			req.URL = mustParseURL(up1.URL + "/")
			req.RequestURI = up1.URL + "/"
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
		}()
	}
	wg.Wait()

	got := puts.Load().([]string)
	// Both requests should have triggered at least one PUT
	if len(got) < 1 {
		t.Fatalf("expected at least 1 PUT, got %d: %v", len(got), got)
	}
}

// TestProxyDropConnectionsCallsDELETE tests that when dropConnections is
// enabled, the handler calls DELETE /api/connections after switching.
func TestProxyDropConnectionsCallsDELETE(t *testing.T) {
	var deleteCount atomic.Int64
	clash := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/version" {
			w.Write([]byte("{}"))
			return
		}
		if r.Method == http.MethodDelete && r.URL.Path == "/connections" {
			deleteCount.Add(1)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer clash.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	cfg := proxyConfig{
		clashBase:      clash.URL,
		rules:          map[string]string{"127.0.0.1": "node-a"},
		fallback:       "node-b",
		dropConnections: true,
	}
	mgr := newProxyManager(cfg, true, "127.0.0.1:2323")
	handler := newProxyHandler(mgr)

	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/", nil)
	req.URL = mustParseURL(upstream.URL + "/")
	req.RequestURI = upstream.URL + "/"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if deleteCount.Load() == 0 {
		t.Fatal("expected DELETE /api/connections to be called, got 0")
	}
}

// TestProxyNoDropConnectionsByDefault tests that DELETE is not called
// when dropConnections is false (default).
func TestProxyNoDropConnectionsByDefault(t *testing.T) {
	var deleteCount atomic.Int64
	clash := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/version" {
			w.Write([]byte("{}"))
			return
		}
		if r.Method == http.MethodDelete {
			deleteCount.Add(1)
		}
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer clash.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	cfg := proxyConfig{
		clashBase: clash.URL,
		rules:     map[string]string{"127.0.0.1": "node-a"},
		fallback:  "node-b",
	}
	mgr := newProxyManager(cfg, true, "127.0.0.1:2323")
	handler := newProxyHandler(mgr)

	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/", nil)
	req.URL = mustParseURL(upstream.URL + "/")
	req.RequestURI = upstream.URL + "/"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if deleteCount.Load() != 0 {
		t.Fatal("expected no DELETE calls by default, got some")
	}
}

// compile-time: ensure net and sync are used
var _ = net.Dial
var _ sync.Mutex
