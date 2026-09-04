// Multi-port proxy mode: one local HTTP proxy port per VPNCheap node.
// Each port is pinned to its node — request comes in → PUT selector to
// that node → forward. No domain routing; 9router does domain→port.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// multiPortManager owns N proxy listeners, one per node.
type multiPortManager struct {
	mu        sync.RWMutex
	clashBase string
	logger    *slog.Logger
	ports     []nodePort
	switchMu  sync.Mutex // serializes selector switches
	lastNode  string
}

type nodePort struct {
	Tag  string `json:"tag"`
	Name string `json:"name"`
	Port int    `json:"port"`
}

func newMultiPortManager(clashBase string, logger *slog.Logger) *multiPortManager {
	return &multiPortManager{
		clashBase: clashBase,
		logger:    logger,
	}
}

// Start waits for the Clash API to be ready, then opens one proxy port
// per node. Non-blocking: returns immediately, ports populate async.
func (m *multiPortManager) Start(basePort int) error {
	go func() {
		// Wait for Clash API to be reachable (autostart may still be running).
		for i := 0; i < 60; i++ {
			if clashReady(m.clashBase) {
				break
			}
			time.Sleep(time.Second)
		}
		tags, err := fetchSelectorMembersSimple(m.clashBase, "proxy")
		if err != nil {
			if m.logger != nil {
				m.logger.Error("multiport: failed to fetch nodes after retries", "err", err)
			}
			return
		}
		m.mu.Lock()
		for i, tag := range tags {
			port := basePort + i
			m.ports = append(m.ports, nodePort{Tag: tag, Name: tag, Port: port})
			go m.startPort(port, tag)
		}
		count := len(m.ports)
		m.mu.Unlock()
		if m.logger != nil {
			m.logger.Info("multiport started", "ports", count, "base", basePort)
		}
	}()
	return nil
}

func (m *multiPortManager) startPort(port int, tag string) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			m.handleCONNECT(w, r, tag)
		} else {
			m.handleHTTP(w, r, tag)
		}
	})
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		if m.logger != nil {
			m.logger.Warn("multiport: port bind failed", "port", port, "tag", tag, "err", err)
		}
		return
	}
	if err := srv.Serve(ln); err != nil {
		if m.logger != nil {
			m.logger.Warn("multiport: serve stopped", "port", port, "err", err)
		}
	}
}

func (m *multiPortManager) handleHTTP(w http.ResponseWriter, r *http.Request, tag string) {
	if err := m.switchTo(tag); err != nil {
		http.Error(w, "selector switch failed: "+err.Error(), http.StatusBadGateway)
		return
	}
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

func (m *multiPortManager) handleCONNECT(w http.ResponseWriter, r *http.Request, tag string) {
	if err := m.switchTo(tag); err != nil {
		http.Error(w, "selector switch failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	target, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		target.Close()
		http.Error(w, "no hijacker", http.StatusInternalServerError)
		return
	}
	client, _, err := hj.Hijack()
	if err != nil {
		target.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
	go io.Copy(target, client)
	io.Copy(client, target)
	target.Close()
	client.Close()
}

func (m *multiPortManager) switchTo(tag string) error {
	m.switchMu.Lock()
	defer m.switchMu.Unlock()
	if m.lastNode == tag {
		return nil
	}
	if err := putSelector(m.clashBase, "proxy", tag); err != nil {
		return err
	}
	m.lastNode = tag
	if m.logger != nil {
		m.logger.Info("multiport: switched selector", "node", tag)
	}
	return nil
}

// export9router outputs one http://127.0.0.1:PORT per line.
func (m *multiPortManager) export9router(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var lines []string
	for _, p := range m.ports {
		lines = append(lines, fmt.Sprintf("http://127.0.0.1:%d", p.Port))
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=nodes_9router.txt")
	fmt.Fprint(w, strings.Join(lines, "\n"))
}

// statusJSON returns the multi-port status.
func (m *multiPortManager) statusJSON(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"running":   len(m.ports) > 0,
		"ports":     len(m.ports),
		"last_node": m.lastNode,
		"list":      m.ports,
	})
}
