package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestProxyRulesAPIGET returns the current rules + fallback
func TestProxyRulesAPIGET(t *testing.T) {
	clash, _ := fakeClashAPI(t, []string{"node-a", "node-b"})
	defer clash.Close()

	cfg := proxyConfig{
		clashBase: clash.URL,
		rules:     map[string]string{"ipinfo.io": "node-b"},
		fallback:  "node-a",
	}
	mgr := newProxyManager(cfg, true, "127.0.0.1:2323")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/rules", nil)
	mgr.rulesHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp proxyRulesFile
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Fallback != "node-a" {
		t.Fatalf("fallback = %q, want node-a", resp.Fallback)
	}
	if len(resp.Rules) != 1 {
		t.Fatalf("rules count = %d, want 1", len(resp.Rules))
	}
	if resp.Rules[0].Domain != "ipinfo.io" || resp.Rules[0].Node != "node-b" {
		t.Fatalf("rules[0] = %+v, want ipinfo.io/node-b", resp.Rules[0])
	}
}

// TestProxyRulesAPIPUT replaces the mapping with valid tags
func TestProxyRulesAPIPUTValid(t *testing.T) {
	clash, _ := fakeClashAPI(t, []string{"node-a", "node-b", "node-c"})
	defer clash.Close()

	cfg := proxyConfig{
		clashBase: clash.URL,
		rules:     map[string]string{},
		fallback:  "node-a",
	}
	mgr := newProxyManager(cfg, true, "127.0.0.1:2323")

	body := bytes.NewReader([]byte(`{
		"fallback": "node-b",
		"rules": [{"domain": "openai.com", "node": "node-c"}]
	}`))
	req := httptest.NewRequest(http.MethodPut, "/api/proxy/rules", body)
	rec := httptest.NewRecorder()
	mgr.rulesHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the change took effect
	mgr.mu.RLock()
	got := mgr.cfg.fallback
	mu_rules := mgr.cfg.rules
	mgr.mu.RUnlock()
	if got != "node-b" {
		t.Fatalf("fallback = %q, want node-b", got)
	}
	if mu_rules["openai.com"] != "node-c" {
		t.Fatalf("rules[openai.com] = %q, want node-c", mu_rules["openai.com"])
	}
}

// TestProxyRulesAPIPUTUnknownNode rejects with 400
func TestProxyRulesAPIPUTUnknownNode(t *testing.T) {
	clash, _ := fakeClashAPI(t, []string{"node-a", "node-b"})
	defer clash.Close()

	cfg := proxyConfig{
		clashBase: clash.URL,
		rules:     map[string]string{},
		fallback:  "node-a",
	}
	mgr := newProxyManager(cfg, true, "127.0.0.1:2323")

	body := bytes.NewReader([]byte(`{
		"fallback": "node-a",
		"rules": [{"domain": "test.com", "node": "node-zzzz"}]
	}`))
	req := httptest.NewRequest(http.MethodPut, "/api/proxy/rules", body)
	rec := httptest.NewRecorder()
	mgr.rulesHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestProxyRulesAPIPUTBadJSON rejects malformed body
func TestProxyRulesAPIPUTBadJSON(t *testing.T) {
	clash, _ := fakeClashAPI(t, []string{"node-a"})
	defer clash.Close()

	cfg := proxyConfig{clashBase: clash.URL, rules: map[string]string{}, fallback: "node-a"}
	mgr := newProxyManager(cfg, true, "127.0.0.1:2323")

	req := httptest.NewRequest(http.MethodPut, "/api/proxy/rules", bytes.NewReader([]byte(`bad`)))
	rec := httptest.NewRecorder()
	mgr.rulesHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad JSON, got %d", rec.Code)
	}
}

// TestProxyStatusAPI returns running + addr + last_node
func TestProxyStatusAPI(t *testing.T) {
	clash, _ := fakeClashAPI(t, []string{"node-a"})
	defer clash.Close()

	cfg := proxyConfig{clashBase: clash.URL, rules: map[string]string{}, fallback: "node-a"}
	mgr := newProxyManager(cfg, true, "127.0.0.1:2323")

	req := httptest.NewRequest(http.MethodGet, "/api/proxy/status", nil)
	rec := httptest.NewRecorder()
	mgr.statusHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["running"] != true {
		t.Fatalf("running = %v, want true", resp["running"])
	}
	if resp["addr"] != "127.0.0.1:2323" {
		t.Fatalf("addr = %v, want 127.0.0.1:2323", resp["addr"])
	}
}
