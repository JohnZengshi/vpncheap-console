package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
)

const vpnBundleID = "com.vpncheap.macnative"

// tunnelHandler controls the macOS NetworkExtension VPN via scutil. The service
// name is resolved from scutil --nc list by matching the bundle id and is never
// taken from request input. Disconnect requires an explicit in-UI confirm.
func tunnelHandler(logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("action") {
		case "status":
			name, state := tunnelState(logger)
			json.NewEncoder(w).Encode(map[string]string{"name": name, "state": state})
		case "connect":
			name := resolveServiceName(logger)
			if name == "" {
				http.Error(w, "vpn service not found", http.StatusNotFound)
				return
			}
			if err := exec.Command("scutil", "--nc", "start", name).Run(); err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"name": name, "action": "connect"})
		case "disconnect":
			name := resolveServiceName(logger)
			if name == "" {
				http.Error(w, "vpn service not found", http.StatusNotFound)
				return
			}
			if err := exec.Command("scutil", "--nc", "stop", name).Run(); err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"name": name, "action": "disconnect"})
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
		}
	})
}

func resolveServiceName(logger *slog.Logger) string {
	out, err := exec.Command("scutil", "--nc", "list").Output()
	if err != nil {
		logger.Warn("scutil list failed", "err", err)
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, vpnBundleID) {
			fields := strings.SplitN(line, "\"", 3)
			if len(fields) >= 2 {
				return fields[1]
			}
		}
	}
	return ""
}

func tunnelState(logger *slog.Logger) (string, string) {
	name := resolveServiceName(logger)
	if name == "" {
		return "", "NotFound"
	}
	out, err := exec.Command("scutil", "--nc", "status", name).Output()
	if err != nil {
		return name, "Unknown"
	}
	state := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	return name, state
}
