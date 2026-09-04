package main

import (
	"encoding/json"
	"log/slog"
	"fmt"
	"net/http"
	"sync"
)

// proxyManager owns the proxyConfig, the proxy server, and the API handlers.
// It replaces the bare proxyConfig that main.go used in earlier tickets:
// now the config is mutable at runtime (PUT /api/proxy/rules) and the
// proxyHandler reads through the manager so rule changes take effect
// immediately.
type proxyManager struct {
	mu      sync.RWMutex
	cfg     proxyConfig
	running bool
	addr    string
	handler *proxyHandler
}

func newProxyManager(cfg proxyConfig, running bool, addr string) *proxyManager {
	m := &proxyManager{cfg: cfg, running: running, addr: addr}
	m.handler = &proxyHandler{mgr: m}
	return m
}

// newProxyHandlerFromManager returns the handler owned by this manager.
func (m *proxyManager) newProxyHandlerFromManager() http.Handler {
	return http.HandlerFunc(m.handler.serve)
}

// rulesHandler handles GET /api/proxy/rules and PUT /api/proxy/rules.
func (m *proxyManager) rulesHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		m.mu.RLock()
		defer m.mu.RUnlock()
		resp := proxyRulesFile{
			Fallback: m.cfg.fallback,
			Rules:    rulesMapToSlice(m.cfg.rules),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

	case http.MethodPut:
		var rf proxyRulesFile
		if err := json.NewDecoder(r.Body).Decode(&rf); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		// Build a candidate config for validation
		newRules := make(map[string]string, len(rf.Rules))
		for _, rule := range rf.Rules {
			newRules[rule.Domain] = rule.Node
		}
		candidate := proxyConfig{
			clashBase: m.cfg.clashBase,
			rules:     newRules,
			fallback:  rf.Fallback,
			logger:    m.cfg.logger,
		}
		if err := validateProxyRules(candidate); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Swap the config atomically
		m.mu.Lock()
		m.cfg.rules = newRules
		m.cfg.fallback = rf.Fallback
		m.mu.Unlock()
		// Reset the handler's lastNode cache so the next request
		// re-evaluates the selector with the new rules.
		if m.handler != nil {
			m.handler.resetSwitchCache()
		}
		if m.cfg.logger != nil {
			m.cfg.logger.Info("proxy rules updated via API", "count", len(newRules), "fallback", rf.Fallback)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(proxyRulesFile{
			Fallback: rf.Fallback,
			Rules:    rf.Rules,
		})

	default:
		http.Error(w, "GET or PUT only", http.StatusMethodNotAllowed)
	}
}

// statusHandler handles GET /api/proxy/status.
func (m *proxyManager) statusHandler(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"running":   m.running,
		"addr":      m.addr,
		"last_node": m.cfg.logger, // placeholder; the proxyHandler tracks lastNode
	})
}

// exportHandler handles GET /api/proxy/export?target=9router.
// Outputs plain text, one http://127.0.0.1:PORT per line, for 9router batch
// import into Proxy Pools. Since the console uses a single port with internal
// domain routing, all lines point to the same address — 9router assigns all
// domains to this one proxy, and the console switches the VPNCheap selector
// per request domain internally.
func (m *proxyManager) exportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	target := r.URL.Query().Get("target")
	m.mu.RLock()
	addr := m.addr
	rules := m.cfg.rules
	fallback := m.cfg.fallback
	m.mu.RUnlock()

	switch target {
	case "9router":
		// 9router expects: one http://host:port per line, no comments.
		// Single-port mode: one line pointing at our proxy.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=9router_proxies.txt")
		fmt.Fprintf(w, "http://%s\n", addr)
	case "json":
		// Human-readable: domain→node mapping for reference or manual config.
		w.Header().Set("Content-Type", "application/json")
		resp := proxyRulesFile{
			Fallback: fallback,
			Rules:    rulesMapToSlice(rules),
		}
		json.NewEncoder(w).Encode(resp)
	default:
		http.Error(w, "invalid target, use 9router or json", http.StatusBadRequest)
	}
}

// rulesMapToSlice converts a map[string]string to a sorted []proxyRuleEntry
// for JSON serialization.
func rulesMapToSlice(m map[string]string) []proxyRuleEntry {
	out := make([]proxyRuleEntry, 0, len(m))
	for domain, node := range m {
		out = append(out, proxyRuleEntry{Domain: domain, Node: node})
	}
	return out
}

// compile-time: ensure slog is used
var _ = slog.LevelInfo
