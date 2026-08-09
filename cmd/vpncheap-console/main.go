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
	"syscall"
	"time"
)

//go:embed web
var webFiles embed.FS

func main() {
	addr := flag.String("addr", "127.0.0.1:18090", "console listen address (loopback only)")
	clash := flag.String("clash", "http://127.0.0.1:9090", "local Clash API base URL")
	flag.Parse()

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

	proxy := httputil.NewSingleHostReverseProxy(clashURL)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Warn("clash api unreachable", "path", r.URL.Path, "err", err)
		http.Error(w, "clash api unreachable: "+err.Error(), http.StatusBadGateway)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", proxy))
	mux.Handle("/tunnel", tunnelHandler(logger))

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
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	srv.Shutdown(shutCtx)
}
