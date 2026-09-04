package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
)

// labelFiles is the ordered list of sing-box config paths to try for labels.
// It starts as defaultLabelCandidates and may be overridden via -labels.
var labelFiles = defaultLabelCandidates

func labelPaths() []string { return labelFiles }

// defaultLabelCandidates are likely locations of a sing-box config whose
// outbounds carry human-readable node tags in the same order the xboard
// subscription delivers them (and therefore the same order the Clash API
// lists xboard_* members). easy_proxies keeps one next to this repo; the
// SFM app container mirrors it.
var defaultLabelCandidates = []string{
	"/Users/john/MY_PROJECT_2026/easy_proxies/easy-proxies-sing-box.json",
	"/Users/john/Library/Group Containers/P8XK3KHB48.io.nekohasekai.sfamt/configs/config_1.json",
}

// singBoxNodeTypes are the outbound kinds that correspond to real servers.
var singBoxNodeTypes = map[string]bool{
	"hysteria2": true, "anytls": true, "shadowsocks": true,
	"tuic": true, "vmess": true, "vless": true, "trojan": true,
}

// loadNodeTags reads a sing-box config and returns the human-readable tags of
// its real-server outbounds, in file order. Returns nil on any failure so
// callers can fall back to raw xboard names.
func loadNodeTags(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg struct {
		Outbounds []struct {
			Type string `json:"type"`
			Tag  string `json:"tag"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil
	}
	var tags []string
	for _, o := range cfg.Outbounds {
		if singBoxNodeTypes[o.Type] {
			tags = append(tags, o.Tag)
		}
	}
	return tags
}

// fetchProxyOrder returns the ordered member list of the "proxy" selector
// (xboard_* names), excluding "direct".
func fetchProxyOrder(clashBase string) ([]string, error) {
	resp, err := http.Get(clashBase + "/proxies/" + "proxy")
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
	names := make([]string, 0, len(body.All))
	for _, n := range body.All {
		if n != "direct" && n != "auto" {
			names = append(names, n)
		}
	}
	return names, nil
}

// labelsHandler builds a positional xboard-name -> human-label mapping by
// pairing the Clash API's proxy member order with the node tags of a readable
// sing-box config. If the config is missing or lengths disagree it returns an
// empty mapping and the UI falls back to raw names.
func labelsHandler(logger *slog.Logger, clashBase string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		apiOrder, err := fetchProxyOrder(clashBase)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]any{"mapping": map[string]string{}})
			return
		}
		var tags []string
		for _, p := range labelPaths() {
			if t := loadNodeTags(p); len(t) > 0 {
				tags = t
				break
			}
		}
		mapping := map[string]string{}
		if len(tags) == len(apiOrder) {
			for i, name := range apiOrder {
				mapping[name] = tags[i]
			}
			if logger != nil {
				logger.Info("labels: mapped", "count", len(mapping), "source", "sing-box config")
			}
		} else if len(tags) > 0 && logger != nil {
			logger.Warn("labels: length mismatch, falling back", "api", len(apiOrder), "tags", len(tags))
		}
		json.NewEncoder(w).Encode(map[string]any{"mapping": mapping})
	})
}
