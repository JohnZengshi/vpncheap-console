package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeClashAPI records PUT /proxies/proxy calls and returns 204.
// GET /proxies/proxy returns a selector with the given members.
func fakeClashAPI(t *testing.T, members []string) (*httptest.Server, *atomic.Value) {
	t.Helper()
	var mu sync.Mutex
	puts := []string{}
	var putsVal atomic.Value
	putsVal.Store([]string{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/version":
			w.Write([]byte(`{"version":"sing-box test"}`))
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/proxies/proxy"):
			var body struct {
				Name string `json:"name"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			puts = append(puts, body.Name)
			putsVal.Store(append([]string(nil), puts...))
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/proxies/proxy"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"now": "node-a",
				"all": members,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return srv, &putsVal
}

func TestProxyHandlerSwitchesNodeForMappedDomain(t *testing.T) {
	clash, puts := fakeClashAPI(t, []string{"node-a", "node-b", "node-c"})
	defer clash.Close()

	cfg := proxyConfig{
		clashBase: clash.URL,
		rules: map[string]string{
			"ipinfo.io": "node-b",
		},
		fallback: "node-a",
	}

	// fake upstream that the proxy forwards to
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	handler := newProxyHandler(newProxyManager(cfg, true, "127.0.0.1:2323"))

	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/test", nil)
	req.Host = "ipinfo.io"
	// for proxy, RequestURI must be set (full URL)
	req.URL, _ = url.Parse(upstream.URL + "/test")
	req.RequestURI = upstream.URL + "/test"

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got := puts.Load().([]string)
	if len(got) == 0 {
		t.Fatal("expected at least one PUT to clash API, got none")
	}
	if got[0] != "node-b" {
		t.Fatalf("expected PUT to node-b for ipinfo.io, got %q", got[0])
	}
}

func TestProxyHandlerUsesFallbackForUnmappedDomain(t *testing.T) {
	clash, puts := fakeClashAPI(t, []string{"node-a", "node-b"})
	defer clash.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	cfg := proxyConfig{
		clashBase: clash.URL,
		rules:     map[string]string{"ipinfo.io": "node-b"},
		fallback:  "node-a",
	}
	handler := newProxyHandler(newProxyManager(cfg, true, "127.0.0.1:2323"))

	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/test", nil)
	req.URL, _ = url.Parse(upstream.URL + "/test")
	req.RequestURI = upstream.URL + "/test"

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	got := puts.Load().([]string)
	if len(got) == 0 || got[0] != "node-a" {
		t.Fatalf("expected PUT to fallback node-a, got %v", got)
	}
}

func TestProxyHandlerSuffixMatch(t *testing.T) {
	clash, puts := fakeClashAPI(t, []string{"node-a", "node-b"})
	defer clash.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	cfg := proxyConfig{
		clashBase: clash.URL,
		rules:     map[string]string{"openai.com": "node-b"},
		fallback:  "node-a",
	}
	handler := newProxyHandler(newProxyManager(cfg, true, "127.0.0.1:2323"))

	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/", nil)
	req.URL, _ = url.Parse(upstream.URL + "/")
	req.RequestURI = upstream.URL + "/"
	req.Host = "chat.openai.com"

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	got := puts.Load().([]string)
	if len(got) == 0 || got[0] != "node-b" {
		t.Fatalf("expected suffix match to node-b for chat.openai.com, got %v", got)
	}
}

func TestProxyHandlerErrorOnSwitchFailure(t *testing.T) {
	// Clash API that always returns 500 for PUT
	clash := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusInternalServerError)
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
		clashBase: clash.URL,
		rules:     map[string]string{"ipinfo.io": "node-b"},
		fallback:  "node-a",
	}
	handler := newProxyHandler(newProxyManager(cfg, true, "127.0.0.1:2323"))

	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/test", nil)
	req.URL, _ = url.Parse(upstream.URL + "/test")
	req.RequestURI = upstream.URL + "/test"
	req.Host = "ipinfo.io"

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on switch failure, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "selector switch failed") {
		t.Fatalf("expected error message about switch failure, got %q", body)
	}
}

func TestProxyHandlerSkipSwitchWhenSameNode(t *testing.T) {
	clash, puts := fakeClashAPI(t, []string{"node-a", "node-b"})
	defer clash.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	cfg := proxyConfig{
		clashBase: clash.URL,
		rules:     map[string]string{"ipinfo.io": "node-b"},
		fallback:  "node-a",
	}
	handler := newProxyHandler(newProxyManager(cfg, true, "127.0.0.1:2323"))

	// first request switches to node-b
	req1 := httptest.NewRequest(http.MethodGet, upstream.URL+"/", nil)
	req1.URL, _ = url.Parse(upstream.URL + "/")
	req1.RequestURI = upstream.URL + "/"
	req1.Host = "ipinfo.io"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// second request for same domain — should NOT switch again
	req2 := httptest.NewRequest(http.MethodGet, upstream.URL+"/", nil)
	req2.URL, _ = url.Parse(upstream.URL + "/")
	req2.RequestURI = upstream.URL + "/"
	req2.Host = "ipinfo.io"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	got := puts.Load().([]string)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 PUT (skip on same node), got %d: %v", len(got), got)
	}
}

// compile-time: ensure we use io and json in test helpers
var _ = io.Discard
