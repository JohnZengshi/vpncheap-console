package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// exitProbeURL returns geolocation JSON for the caller's egress IP. Because
// the VPN tunnel uses auto_route, a request from this process exits through
// the tunnel when it is connected, and through the local network otherwise.
// It is a variable so tests can point it at a local server.
var exitProbeURL = "https://ipinfo.io/json"

// exitProbeClient must not keep connections alive. exitHandler is polled
// every few seconds, and once the proxy selector switches node, a pooled
// keep-alive connection to exitProbeURL keeps routing through the *old*
// node's tunnel session until it naturally expires. That made the reported
// exit IP lag one node switch behind, which looked like nodes reporting the
// wrong country/city. Disabling keep-alives forces a fresh connection (and
// therefore fresh routing through the current selector) on every check.
var exitProbeClient = &http.Client{
	Transport: &http.Transport{DisableKeepAlives: true},
}

type exitInfo struct {
	IP      string `json:"ip"`
	City    string `json:"city"`
	Region  string `json:"region"`
	Country string `json:"country"`
	Org     string `json:"org"`
}

// exitHandler reports where the current egress IP actually is, so the user can
// verify a node's real exit location instead of trusting its label (some
// airport "TW" nodes actually exit in Japan). It also reports the tunnel state
// so the UI can tell a tunnel egress from a bare local-network egress.
func exitHandler(logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Ask the tunnel controller for its state; the probe result is only
		// meaningful as an egress check when the tunnel is connected.
		name, state := tunnelState(logger)

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, exitProbeURL, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		req.Header.Set("User-Agent", "curl/8.0") // ipinfo returns plain JSON for curl UA
		resp, err := exitProbeClient.Do(req)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]any{
				"tunnel": map[string]string{"name": name, "state": state},
				"error":  "probe failed: " + err.Error(),
			})
			return
		}
		defer resp.Body.Close()

		var info exitInfo
		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			json.NewEncoder(w).Encode(map[string]any{
				"tunnel": map[string]string{"name": name, "state": state},
				"error":  "decode failed: " + err.Error(),
			})
			return
		}
		if logger != nil {
			logger.Info("exit: probed", "ip", info.IP, "country", info.Country, "city", info.City, "tunnel", state)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"tunnel":  map[string]string{"name": name, "state": state},
			"ip":      info.IP,
			"city":    info.City,
			"region":  info.Region,
			"country": info.Country,
			"org":     info.Org,
		})
	})
}
