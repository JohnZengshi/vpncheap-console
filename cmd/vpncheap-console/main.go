package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

//go:embed web
var webFiles embed.FS

func main() {
	addr := flag.String("addr", "127.0.0.1:18090", "console listen address (loopback only)")
	clash := flag.String("clash", "http://127.0.0.1:9090", "local Clash API base URL")
	autostartFlag := flag.Bool("autostart", true, "launch VPNCheap and connect tunnel if the Clash API is not reachable")
	pidfilePath := flag.String("pidfile", filepath.Join(os.Getenv("HOME"), ".vpncheap-console.pid"), "pidfile path for make stop")
	labelsFlag := flag.String("labels", "", "comma-separated sing-box config paths for human-readable node labels (default: auto-detect easy_proxies/SFM configs)")
	flag.Parse()

	if *labelsFlag != "" {
		var paths []string
		for _, p := range strings.Split(*labelsFlag, ",") {
			if p = strings.TrimSpace(p); p != "" {
				paths = append(paths, p)
			}
		}
		if len(paths) > 0 {
			labelFiles = paths
		}
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	clashURL, err := url.Parse(*clash)
	if err != nil {
		logger.Error("invalid -clash url", "err", err)
		os.Exit(1)
	}
	host, _, _ := net.SplitHostPort(*addr)
	if host != "" && host != "localhost" && host != "127.0.0.1" && host != "::1" {
		logger.Error("-addr must bind loopback only", "addr", *addr)
		os.Exit(1)
	}

	if err := writePidfile(*pidfilePath); err != nil {
		logger.Warn("write pidfile failed", "err", err)
	}
	defer removePidfile(*pidfilePath)

	proxy := httputil.NewSingleHostReverseProxy(clashURL)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Warn("clash api unreachable", "path", r.URL.Path, "err", err)
		http.Error(w, "clash api unreachable: "+err.Error(), http.StatusBadGateway)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", proxy))
	mux.Handle("/tunnel", tunnelHandler(logger))
	mux.Handle("/best", bestHandler(logger, *clash))
	mux.Handle("/health", healthHandler())
	mux.Handle("/labels", labelsHandler(logger, *clash))

	webRoot, err := fs.Sub(webFiles, "web")
	if err != nil {
		logger.Error("embed sub failed", "err", err)
		os.Exit(1)
	}
	mux.Handle("/", http.FileServerFS(webRoot))

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start HTTP immediately and run autostart in the background so the UI can
	// report phase via /health while we wait for the Clash API.
	if *autostartFlag {
		go autostart(logger, *clash)
	} else {
		setPhase("ready", "autostart disabled")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go func() {
		logger.Info("console starting", "addr", *addr, "clash", *clash)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "err", err)
			os.Exit(1)
		}
	}()
	<-ctx.Done()
	logger.Info("shutting down...")
	// Per ADR-0002: full shutdown — disconnect tunnel + quit VPNCheap, then stop
	// the HTTP server. Never force-kill the system extension.
	shutdownVPN(logger)
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	srv.Shutdown(shutCtx)
}
