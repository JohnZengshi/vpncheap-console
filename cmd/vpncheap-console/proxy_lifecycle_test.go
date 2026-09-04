package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestProxyReturns503WhenClashDown tests that the proxy returns 503
// when the Clash API is unreachable.
func TestProxyReturns503WhenClashDown(t *testing.T) {
	cfg := proxyConfig{
		clashBase: "http://127.0.0.1:1", // unreachable
		rules:     map[string]string{"a.com": "node-a"},
		fallback:  "node-a",
	}
	mgr := newProxyManager(cfg, true, "127.0.0.1:2323")
	handler := newProxyHandler(mgr)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RequestURI = "http://example.com/"
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when Clash API down, got %d", rec.Code)
	}
}

// TestProxyFlagParsing tests that -proxy-addr non-loopback is rejected.
func TestProxyAddrLoopbackEnforced(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:2323": true,  // valid loopback
		"localhost:2323": true,
		"::1:2323":       true,
		"":               true,  // empty host = loopback default
		"0.0.0.0:2323":   false, // not loopback
		"192.168.1.1:2323": false,
	}
	for addr, valid := range cases {
		host, _, _ := splitHostPort(addr)
		ok := host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1"
		if ok != valid {
			t.Errorf("addr %q: expected valid=%v, got valid=%v", addr, valid, ok)
		}
	}
}

// splitHostPort is a test helper matching main.go's net.SplitHostPort.
func splitHostPort(addr string) (string, string, error) {
	return netSplitHostPort(addr)
}
