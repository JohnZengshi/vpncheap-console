// PROTOTYPE — throwaway. Answers: "can console run an HTTP proxy that, per
// request domain, switches VPNCheap's selector so different domains exit
// through different nodes?"
//
// Run:  go run ./prototype/domain-router
// Then: curl -x http://127.0.0.1:2323 http://ipinfo.io/json
//       curl -x http://127.0.0.1:2323 https://ifconfig.me
//
// The mapping (domain suffix -> node tag) is hardcoded. Each request:
// 1. parse target domain
// 2. PUT /proxies/proxy {name: mapped_node}
// 3. proxy the request (traffic exits via VPNCheap TUN -> selector -> node)
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	clashAPI   = "http://127.0.0.1:9090"
	selector   = "proxy"
	listenAddr = "127.0.0.1:2323"
)

// domain -> node mapping (hardcoded for prototype)
var domainMap = map[string]string{
	// ipinfo.io      -> HK node
	"ipinfo.io": "xboard_96b35930b860b2e5",
	// ifconfig.me    -> JP node
	"ifconfig.me": "xboard_4108327100f34551",
}

// fallback node for unmapped domains
const fallbackNode = "xboard_96b35930b860b2e5"

var (
	mu       sync.Mutex
	lastNode string
)

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	log.Printf("[PROTOTYPE] domain-router proxy starting on %s", listenAddr)
	log.Printf("[PROTOTYPE] mapping: %v", domainMap)
	log.Printf("[PROTOTYPE] fallback: %s", fallbackNode)
	log.Println("[PROTOTYPE] test with:")
	log.Println("  curl -x http://127.0.0.1:2323 http://ipinfo.io/json")
	log.Println("  curl -x http://127.0.0.1:2323 https://ifconfig.me")

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: http.HandlerFunc(handleProxy),
		// long timeouts — we're testing, not serving production
		ReadHeaderTimeout: 15 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}

func handleProxy(w http.ResponseWriter, r *http.Request) {
	// Step 1: determine target domain
	domain := extractDomain(r.Host)
	if domain == "" {
		domain = r.Host
	}

	// Step 2: look up node for this domain
	node, ok := domainMap[domain]
	if !ok {
		// try suffix match
		for suffix, n := range domainMap {
			if strings.HasSuffix(domain, suffix) {
				node = n
				ok = true
				break
			}
		}
	}
	if !ok {
		node = fallbackNode
	}

	// Step 3: switch selector (serialize to avoid race)
	mu.Lock()
	if lastNode != node {
		err := switchSelector(node)
		if err != nil {
			mu.Unlock()
			log.Printf("[ERR] switch to %s for %s: %v", node, domain, err)
			http.Error(w, "selector switch failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		lastNode = node
		log.Printf("[SWITCH] %s -> node %s", domain, node)
	}
	mu.Unlock()

	// Step 4: proxy the request
	if r.Method == http.MethodConnect {
		handleHTTPS(w, r)
	} else {
		handleHTTP(w, r)
	}
}

func extractDomain(host string) string {
	// strip port
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	return host
}

func switchSelector(node string) error {
	payload, _ := json.Marshal(map[string]string{"name": node})
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
	return nil
}

func handleHTTP(w http.ResponseWriter, r *http.Request) {
	// standard http forward proxy
	r.RequestURI = ""
	// remove hop-by-hop headers
	r.Header.Del("Proxy-Connection")
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		log.Printf("[ERR] forward %s: %v", r.URL, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	// copy headers
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func handleHTTPS(w http.ResponseWriter, r *http.Request) {
	// CONNECT method — open a tunnel to the target
	target, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
	if err != nil {
		log.Printf("[ERR] dial %s: %v", r.Host, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusOK)
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "no hijacker", http.StatusInternalServerError)
		target.Close()
		return
	}
	client, _, err := hj.Hijack()
	if err != nil {
		log.Printf("[ERR] hijack: %v", err)
		target.Close()
		return
	}
	go io.Copy(target, client)
	io.Copy(client, target)
	target.Close()
	client.Close()
}

// helper: show current selector state
func init() {
	// print current node at startup
	resp, err := http.Get(clashAPI + "/proxies/" + selector)
	if err == nil {
		defer resp.Body.Close()
		var d struct {
			Now string `json:"now"`
		}
		json.NewDecoder(resp.Body).Decode(&d)
		lastNode = d.Now
		log.Printf("[PROTOTYPE] current selector now=%s", lastNode)
	}
	// silence "imported and not used" for url
	_ = url.URL{}
}
