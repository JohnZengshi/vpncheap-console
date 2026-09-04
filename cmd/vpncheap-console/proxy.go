// Package main: domain-based proxy router.
//
// When enabled (-proxy flag), the console starts a second HTTP server on a
// loopback port that acts as a forward proxy. For each request, it looks up
// the target domain in a mapping, switches VPNCheap's selector to the mapped
// node via the Clash API, then forwards the request. Traffic exits through
// VPNCheap's TUN, which uses the just-switched selector.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// proxyConfig holds the domain→node mapping and Clash API base URL.
type proxyConfig struct {
	clashBase string
	rules     map[string]string // domain suffix → node tag
	fallback  string            // node tag for unmapped domains
	dropConnections bool
	logger    *slog.Logger
}

// proxyHandler is the HTTP handler that switches the selector and forwards.
type proxyHandler struct {
	mgr      *proxyManager
	mu       sync.Mutex // serializes switch to avoid selector race
	lastNode string
}

func newProxyHandler(mgr *proxyManager) http.Handler {
	h := &proxyHandler{mgr: mgr}
	return http.HandlerFunc(h.serve)
}

func (h *proxyHandler) serve(w http.ResponseWriter, r *http.Request) {
	// Reject requests if VPNCheap's Clash API is down.
	h.mgr.mu.RLock()
	cb := h.mgr.cfg.clashBase
	h.mgr.mu.RUnlock()
	if !clashReady(cb) {
		http.Error(w, "VPNCheap Clash API unavailable", http.StatusServiceUnavailable)
		return
	}

	// CONNECT method = HTTPS tunnel.
	if r.Method == http.MethodConnect {
		h.handleCONNECT(w, r)
		return
	}

	domain := extractProxyDomain(r)
	node, ok := h.lookupNode(domain)
	if !ok {
		node = h.fallback()
	}

	// Switch selector (serialized; skip if already on this node).
	if err := h.switchIfNeeded(node); err != nil {
		if h.logger() != nil {
			h.logger().Warn("proxy: selector switch failed", "domain", domain, "node", node, "err", err)
		}
		http.Error(w, "selector switch failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Forward as a standard HTTP proxy.
	h.forwardHTTP(w, r)
}

// extractProxyDomain gets the target domain from the request.
func extractProxyDomain(r *http.Request) string {
	if r == nil {
		return ""
	}
	host := r.Host
	if host == "" && r.URL != nil {
		host = r.URL.Host
	}
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	return host
}

// lookupNode finds a node tag by suffix-matching the domain against rules.
func (h *proxyHandler) lookupNode(domain string) (string, bool) {
	h.mgr.mu.RLock()
	rules := h.mgr.cfg.rules
	h.mgr.mu.RUnlock()
	// exact match first
	if node, ok := rules[domain]; ok {
		return node, true
	}
	// suffix match: rule "openai.com" matches "chat.openai.com"
	for suffix, node := range rules {
		if strings.HasSuffix(domain, suffix) {
			return node, true
		}
	}
	return "", false
}

// fallback returns the configured fallback node tag (thread-safe).
func (h *proxyHandler) fallback() string {
	h.mgr.mu.RLock()
	defer h.mgr.mu.RUnlock()
	return h.mgr.cfg.fallback
}

// logger returns the logger (thread-safe).
func (h *proxyHandler) logger() *slog.Logger {
	h.mgr.mu.RLock()
	defer h.mgr.mu.RUnlock()
	return h.mgr.cfg.logger
}

// switchIfNeeded calls PUT /proxies/proxy if the node changed, skipping if
// already on the same node (avoids unnecessary Clash API calls).
func (h *proxyHandler) switchIfNeeded(node string) error {
	if node == "" {
		return nil // no mapping and no fallback; skip switch
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.lastNode == node {
		return nil
	}
	h.mgr.mu.RLock()
	cb := h.mgr.cfg.clashBase
	dc := h.mgr.cfg.dropConnections
	h.mgr.mu.RUnlock()
	if err := putSelector(cb, "proxy", node); err != nil {
		return err
	}
	// Optionally drop existing connections so keep-alive sessions rebuild
	// through the new node (same pattern as the /best endpoint).
	if dc {
		if err := deleteConnections(cb); err != nil {
			if h.logger() != nil {
				h.logger().Warn("proxy: delete connections failed", "err", err)
			}
		}
	}
	h.lastNode = node
	if h.logger() != nil {
		h.logger().Info("proxy: switched selector", "node", node, "drop_connections", dc)
	}
	return nil
}

// deleteConnections calls DELETE /api/connections on the Clash API, dropping
// all live connections so they rebuild through the new selector.
func deleteConnections(clashBase string) error {
	req, err := http.NewRequest(http.MethodDelete, clashBase+"/connections", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// forwardHTTP is a minimal HTTP forward proxy: strip hop-by-hop headers,
// set RequestURI to empty (client-side request), and RoundTrip.
func (h *proxyHandler) forwardHTTP(w http.ResponseWriter, r *http.Request) {
	r.RequestURI = ""
	r.Header.Del("Proxy-Connection")
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handleCONNECT handles the HTTPS CONNECT method: switch selector, dial
// the target (goes through VPNCheap's TUN), then tunnel bytes both ways.
func (h *proxyHandler) handleCONNECT(w http.ResponseWriter, r *http.Request) {
	// Reject if VPNCheap is down.
	h.mgr.mu.RLock()
	cb := h.mgr.cfg.clashBase
	h.mgr.mu.RUnlock()
	if !clashReady(cb) {
		http.Error(w, "VPNCheap Clash API unavailable", http.StatusServiceUnavailable)
		return
	}

	domain := extractProxyDomain(r)
	node, ok := h.lookupNode(domain)
	if !ok {
		node = h.fallback()
	}

	if err := h.switchIfNeeded(node); err != nil {
		http.Error(w, "selector switch failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Dial the target host directly. This goes through the system TUN
	// (VPNCheap), which uses the just-switched selector.
	target, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	// Hijack the client connection and send the 200 response on the raw
	// connection. After hijack, w is unusable — no http.Error calls.
	hj, ok := w.(http.Hijacker)
	if !ok {
		target.Close()
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}
	client, _, err := hj.Hijack()
	if err != nil {
		target.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Send the CONNECT response on the hijacked connection.
	fmt.Fprintf(client, "HTTP/1.1 200 Connection Established\r\n\r\n")

	go io.Copy(target, client)
	io.Copy(client, target)
	target.Close()
	client.Close()
}

// putSelector sends PUT /proxies/<selector> with {"name":"<node>"}.
func putSelector(clashBase, selector, node string) error {
	payload, err := json.Marshal(map[string]string{"name": node})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut,
		clashBase+"/proxies/"+selector, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("clash api returned %d switching to %q", resp.StatusCode, node)
	}
	return nil
}

// proxyRulesFile is the JSON schema for the -proxy-rules file.
type proxyRulesFile struct {
	Fallback string           `json:"fallback"`
	Rules    []proxyRuleEntry `json:"rules"`
}

type proxyRuleEntry struct {
	Domain string `json:"domain"`
	Node   string `json:"node"`
}

// loadProxyRules reads a JSON rules file and returns a proxyConfig with
// the mapping populated. If the file does not exist, returns an empty
// config with no error (proxy starts with no rules). Bad JSON returns
// an error.
func loadProxyRules(path string) (proxyConfig, error) {
	cfg := proxyConfig{rules: map[string]string{}}
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil // missing file is not an error
		}
		return cfg, err
	}
	var rf proxyRulesFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return cfg, err
	}
	cfg.fallback = rf.Fallback
	for _, r := range rf.Rules {
		cfg.rules[r.Domain] = r.Node
	}
	return cfg, nil
}

// validateProxyRules checks that every node tag in the rules and the fallback
// exists in VPNCheap's proxy selector.
func validateProxyRules(cfg proxyConfig) error {
	members, err := fetchSelectorMembersSimple(cfg.clashBase, "proxy")
	if err != nil {
		return err
	}
	available := make(map[string]bool, len(members))
	for _, m := range members {
		available[m] = true
	}
	if cfg.fallback != "" && !available[cfg.fallback] {
		return fmt.Errorf("unknown fallback node %q (available: %v)", cfg.fallback, members)
	}
	for domain, node := range cfg.rules {
		if !available[node] {
			return fmt.Errorf("unknown node %q mapped from domain %q (available: %v)", node, domain, members)
		}
	}
	return nil
}

// fetchSelectorMembersSimple fetches the "all" array from the proxy selector.
func fetchSelectorMembersSimple(clashBase, selector string) ([]string, error) {
	resp, err := http.Get(clashBase + "/proxies/" + selector)
	if err != nil {
		return nil, fmt.Errorf("clash api unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clash api returned %d", resp.StatusCode)
	}
	var body struct {
		All []string `json:"all"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.All, nil
}

// mustParseURL is a test helper that panics on bad URLs (test-only).
func mustParseURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

// resetSwitchCache clears the lastNode cache so the next request
// re-evaluates the selector even if it maps to the same node as before.
// Called when rules are updated via PUT /api/proxy/rules.
func (h *proxyHandler) resetSwitchCache() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastNode = ""
}

// netSplitHostPort wraps net.SplitHostPort for testability.
func netSplitHostPort(addr string) (string, string, error) {
	return net.SplitHostPort(addr)
}
