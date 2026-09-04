// PROTOTYPE — throwaway. Answers: "can we open one local port per VPNCheap
// node, each port pinned to its node via selector switch, and export the
// list as http://127.0.0.1:PORT lines for 9router?"
//
// Run:  go run ./prototype/multiport
// Then: curl -x http://127.0.0.1:24000 http://ipinfo.io/json  (node 0)
//       curl -x http://127.0.0.1:24001 http://ipinfo.io/json  (node 1)
//       curl http://127.0.0.1:18091/export                    (9router export)
//
// Each port = one node. Request comes in → PUT selector to that node → forward.
// No domain routing — the port IS the routing (9router does domain→port).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	clashAPI  = "http://127.0.0.1:9090"
	selector  = "proxy"
	basePort  = 24000
	adminAddr = "127.0.0.1:18091"
)

type nodePort struct {
	tag  string
	port int
}

var (
	ports []nodePort
	mu    sync.Mutex
	lastNode string
)

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	// 1. Fetch all node tags from Clash API
	tags, err := fetchSelectorMembers()
	if err != nil {
		log.Fatalf("fetch selector members: %v", err)
	}
	log.Printf("[PROTOTYPE] got %d nodes from Clash API", len(tags))

	// 2. Start one HTTP proxy per node on port basePort+i
	for i, tag := range tags {
		port := basePort + i
		ports = append(ports, nodePort{tag: tag, port: port})
		go startProxyPort(port, tag)
	}
	log.Printf("[PROTOTYPE] started %d proxy ports %d-%d", len(tags), basePort, basePort+len(tags)-1)

	// 3. Admin server: export endpoint
	http.HandleFunc("/export", handleExport)
	http.HandleFunc("/status", handleStatus)
	go func() {
		log.Printf("[PROTOTYPE] admin server on %s", adminAddr)
		if err := http.ListenAndServe(adminAddr, nil); err != nil {
			log.Fatalf("admin server: %v", err)
		}
	}()

	log.Println("[PROTOTYPE] test with:")
	log.Printf("  curl -x http://127.0.0.1:%d http://ipinfo.io/json", basePort)
	log.Printf("  curl -x http://127.0.0.1:%d http://ipinfo.io/json", basePort+1)
	log.Printf("  curl http://127.0.0.1:%s/export", adminAddr)

	// block forever
	select {}
}

func fetchSelectorMembers() ([]string, error) {
	resp, err := http.Get(clashAPI + "/proxies/" + selector)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		All []string `json:"all"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.All, nil
}

func startProxyPort(port int, tag string) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CONNECT = HTTPS
		if r.Method == http.MethodConnect {
			handleProxyCONNECT(w, r, tag)
			return
		}
		// HTTP
		handleProxyHTTP(w, r, tag)
	})
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		log.Printf("[WARN] port %d bind failed (skipping): %v", port, err)
		return
	}
	if err := srv.Serve(ln); err != nil {
		log.Printf("[WARN] port %d serve: %v", port, err)
	}
}

func handleProxyHTTP(w http.ResponseWriter, r *http.Request, tag string) {
	if err := switchTo(tag); err != nil {
		http.Error(w, "switch failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	// forward
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

func handleProxyCONNECT(w http.ResponseWriter, r *http.Request, tag string) {
	if err := switchTo(tag); err != nil {
		http.Error(w, "switch failed: "+err.Error(), http.StatusBadGateway)
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

func switchTo(tag string) error {
	mu.Lock()
	defer mu.Unlock()
	if lastNode == tag {
		return nil // already on this node
	}
	payload, _ := json.Marshal(map[string]string{"name": tag})
	req, _ := http.NewRequest(http.MethodPut, clashAPI+"/proxies/"+selector, strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("clash api returned %d", resp.StatusCode)
	}
	lastNode = tag
	log.Printf("[SWITCH] -> %s", tag)
	return nil
}

func handleExport(w http.ResponseWriter, r *http.Request) {
	var lines []string
	for _, p := range ports {
		lines = append(lines, fmt.Sprintf("http://127.0.0.1:%d", p.port))
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=nodes_9router.txt")
	fmt.Fprint(w, strings.Join(lines, "\n"))
	log.Printf("[EXPORT] %d lines", len(lines))
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"ports":     len(ports),
		"base_port": basePort,
		"last_node": lastNode,
	})
}
