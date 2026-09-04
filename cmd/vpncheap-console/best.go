// bestHandler tests every node in the "proxy" selector against a probe URL
// and switches to whichever one comes back fastest. It fills the gap the
// Clash API reverse proxy doesn't cover: /api/proxies gives you the list and
// /api/proxies/{name}/delay gives you one measurement, but nothing on the
// upstream side picks a winner across all of them and applies it.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const bestSelector = "proxy"
const bestProbeURL = "http://www.gstatic.com/generate_204"
const bestProbeTimeout = 5 * time.Second

type bestResult struct {
	Name  string `json:"name"`
	Delay int    `json:"delay"` // 0 means the probe failed or timed out
}

// bestHandler drives the same Clash API the reverse proxy talks to, but
// server-side and in parallel, then issues the PUT itself. clashBase must be
// the same base URL passed via -clash.
func bestHandler(logger *slog.Logger, clashBase string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		names, err := fetchSelectorMembers(ctx, clashBase, bestSelector)
		if err != nil {
			logger.Warn("best: fetch selector failed", "err", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		results := probeAll(ctx, clashBase, names)

		best := bestResult{Delay: 0}
		for _, res := range results {
			if res.Delay > 0 && (best.Name == "" || res.Delay < best.Delay) {
				best = res
			}
		}

		if best.Name == "" {
			json.NewEncoder(w).Encode(map[string]any{
				"results": results,
				"error":   "every node failed or timed out",
			})
			return
		}

		if err := switchSelector(ctx, clashBase, bestSelector, best.Name); err != nil {
			logger.Warn("best: switch failed", "name", best.Name, "err", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		logger.Info("best: switched", "name", best.Name, "delay", best.Delay)
		json.NewEncoder(w).Encode(map[string]any{
			"results": results,
			"best":    best,
		})
	})
}

func fetchSelectorMembers(ctx context.Context, base, selector string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/proxies/"+url.PathEscape(selector), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clash api returned %d for selector %q", resp.StatusCode, selector)
	}
	var body struct {
		All []string `json:"all"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.All, nil
}

// probeAll tests every node concurrently. A failed or timed-out probe is
// recorded as delay 0 rather than dropped, so the caller sees every node it
// asked about.
func probeAll(ctx context.Context, base string, names []string) []bestResult {
	results := make([]bestResult, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			results[i] = bestResult{Name: name, Delay: probeOne(ctx, base, name)}
		}(i, name)
	}
	wg.Wait()
	return results
}

func probeOne(ctx context.Context, base, name string) int {
	probeCtx, cancel := context.WithTimeout(ctx, bestProbeTimeout)
	defer cancel()

	q := url.Values{}
	q.Set("timeout", fmt.Sprintf("%d", bestProbeTimeout.Milliseconds()))
	q.Set("url", bestProbeURL)
	target := base + "/proxies/" + url.PathEscape(name) + "/delay?" + q.Encode()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, target, nil)
	if err != nil {
		return 0
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0
	}
	var body struct {
		Delay int `json:"delay"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0
	}
	return body.Delay
}

func switchSelector(ctx context.Context, base, selector, name string) error {
	payload, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, base+"/proxies/"+url.PathEscape(selector), bytes.NewReader(payload))
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
		return fmt.Errorf("clash api returned %d switching to %q", resp.StatusCode, name)
	}
	return nil
}
