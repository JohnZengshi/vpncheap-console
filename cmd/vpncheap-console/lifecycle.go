package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

const (
	appBundleName = "VPNCheap"
	autostartWait = 30 * time.Second
	autostartPoll = 500 * time.Millisecond
)

var (
	phaseMu  sync.Mutex
	phase    = "launching"
	phaseMsg = "init"
)

func setPhase(p, msg string) {
	phaseMu.Lock()
	phase = p
	phaseMsg = msg
	phaseMu.Unlock()
}

func getPhase() (string, string) {
	phaseMu.Lock()
	defer phaseMu.Unlock()
	return phase, phaseMsg
}

// healthHandler reports the console's launch phase so the UI can show
// "starting VPNCheap / waiting for kernel" instead of a blank table.
func healthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p, msg := getPhase()
		json.NewEncoder(w).Encode(map[string]string{"phase": p, "detail": msg})
	})
}

// autostart ensures VPNCheap is running and the Clash API is reachable. If the
// API is already up it is a no-op. Otherwise it opens the app (once), starts
// the tunnel, and polls the API for up to autostartWait. Per Q6 it does not
// re-open a crashing app; on timeout it degrades and logs where to find crash
// reports.
func autostart(logger *slog.Logger, clashBase string) {
	if clashReady(clashBase) {
		setPhase("ready", "clash api already reachable")
		logger.Info("autostart: clash api already reachable, skipping")
		return
	}
	setPhase("launching", "starting VPNCheap.app")
	if !appRunning() {
		// -j: launch hidden, -g: do not bring the app to the foreground. The
		// console drives VPNCheap headless; its window/menu-bar UI must not
		// pop up over the user's work.
		if err := exec.Command("open", "-gj", "-a", appBundleName).Run(); err != nil {
			logger.Warn("autostart: open app failed", "err", err)
			setPhase("degraded", "open VPNCheap failed: "+err.Error())
			return
		}
	}
	setPhase("launching", "starting tunnel")
	name := resolveServiceName(logger)
	if name == "" {
		for i := 0; i < 10 && name == ""; i++ {
			time.Sleep(time.Second)
			name = resolveServiceName(logger)
		}
	}
	if name != "" {
		if err := exec.Command("scutil", "--nc", "start", name).Run(); err != nil {
			logger.Warn("autostart: scutil start failed", "err", err)
		}
	} else {
		logger.Warn("autostart: vpn service not found after app launch")
	}
	setPhase("launching", "waiting for clash api (9090)")
	deadline := time.Now().Add(autostartWait)
	for time.Now().Before(deadline) {
		if clashReady(clashBase) {
			setPhase("ready", "clash api reachable")
			logger.Info("autostart: clash api ready")
			return
		}
		time.Sleep(autostartPoll)
	}
	setPhase("degraded", fmt.Sprintf("clash api not reachable after %v; VPNCheap may have crashed, see ~/Library/Logs/DiagnosticReports", autostartWait))
	logger.Warn("autostart: clash api not reachable after wait", "wait", autostartWait)
}

// clashReady returns true if the Clash API answers /version with 200.
func clashReady(clashBase string) bool {
	resp, err := http.Get(clashBase + "/version")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// appRunning reports whether the VPNCheap GUI process is alive.
func appRunning() bool {
	return exec.Command("pgrep", "-x", appBundleName).Run() == nil
}

// shutdownVPN disconnects the tunnel and quits VPNCheap. Called from the
// SIGTERM/SIGINT path so make stop cleans up the whole stack. Per Q7 it never
// force-kills the system extension; scutil stop failure is logged, not fatal.
func shutdownVPN(logger *slog.Logger) {
	logger.Info("shutdown: stopping tunnel")
	if name := resolveServiceName(logger); name != "" {
		if err := exec.Command("scutil", "--nc", "stop", name).Run(); err != nil {
			logger.Warn("shutdown: scutil stop failed", "err", err)
		}
	} else {
		logger.Warn("shutdown: vpn service not found")
	}
	logger.Info("shutdown: quitting VPNCheap")
	if err := exec.Command("osascript", "-e", `tell application "`+appBundleName+`" to quit`).Run(); err != nil {
		logger.Warn("shutdown: quit VPNCheap failed", "err", err)
	}
}

// writePidfile records the current PID so make stop can target this process.
func writePidfile(path string) error {
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644)
}

// readPidfile reads the PID previously written, returning 0 if missing/invalid.
func readPidfile(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(string(b))
	if err != nil {
		return 0
	}
	return pid
}

func removePidfile(path string) { os.Remove(path) }

// watchClash monitors the Clash API in the background. If it becomes
// unreachable (VPNCheap crashed or the tunnel dropped), it re-runs the
// autostart sequence to bring everything back up. This keeps the console
// self-healing instead of spamming "clash api unreachable" forever.
func watchClash(logger *slog.Logger, clashBase string) {
	for {
		time.Sleep(10 * time.Second)
		if clashReady(clashBase) {
			continue
		}
		if _, ph := getPhase(); ph == "launching" {
			continue // autostart already running
		}
		logger.Warn("watchClash: clash api down, re-launching VPNCheap")
		autostart(logger, clashBase)
	}
}
