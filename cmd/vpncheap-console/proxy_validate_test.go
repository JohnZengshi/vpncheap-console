package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateProxyRulesAllValid(t *testing.T) {
	clash, _ := fakeClashAPI(t, []string{"node-a", "node-b", "node-c"})
	defer clash.Close()

	cfg := proxyConfig{
		clashBase: clash.URL,
		rules: map[string]string{
			"ipinfo.io":  "node-b",
			"openai.com": "node-c",
		},
		fallback: "node-a",
	}

	err := validateProxyRules(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateProxyRulesUnknownNodeInRule(t *testing.T) {
	clash, _ := fakeClashAPI(t, []string{"node-a", "node-b"})
	defer clash.Close()

	cfg := proxyConfig{
		clashBase: clash.URL,
		rules: map[string]string{
			"ipinfo.io":  "node-b",
			"google.com": "node-z",
		},
		fallback: "node-a",
	}

	err := validateProxyRules(cfg)
	if err == nil {
		t.Fatal("expected error for unknown node-z, got nil")
	}
	if !strings.Contains(err.Error(), "node-z") {
		t.Fatalf("error should mention unknown tag node-z, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "google.com") {
		t.Fatalf("error should mention domain google.com, got %q", err.Error())
	}
}

func TestValidateProxyRulesUnknownFallback(t *testing.T) {
	clash, _ := fakeClashAPI(t, []string{"node-a", "node-b"})
	defer clash.Close()

	cfg := proxyConfig{
		clashBase: clash.URL,
		rules:     map[string]string{"ipinfo.io": "node-a"},
		fallback:  "node-z",
	}

	err := validateProxyRules(cfg)
	if err == nil {
		t.Fatal("expected error for unknown fallback, got nil")
	}
	if !strings.Contains(err.Error(), "fallback") {
		t.Fatalf("error should mention fallback, got %q", err.Error())
	}
}

func TestValidateProxyRulesEmptyFallbackSkipped(t *testing.T) {
	clash, _ := fakeClashAPI(t, []string{"node-a"})
	defer clash.Close()

	cfg := proxyConfig{
		clashBase: clash.URL,
		rules:     map[string]string{},
		fallback:  "",
	}

	err := validateProxyRules(cfg)
	if err != nil {
		t.Fatalf("expected no error with empty fallback, got %v", err)
	}
}

func TestValidateProxyRulesClashDown(t *testing.T) {
	cfg := proxyConfig{
		clashBase: "http://127.0.0.1:1",
		rules:     map[string]string{"a.com": "node-a"},
		fallback:  "node-a",
	}

	err := validateProxyRules(cfg)
	if err == nil {
		t.Fatal("expected error when Clash API down, got nil")
	}
}

var _ = http.MethodGet
var _ = httptest.NewServer
