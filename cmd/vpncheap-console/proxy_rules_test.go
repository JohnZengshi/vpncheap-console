package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProxyRulesValid(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rules.json")
	os.WriteFile(p, []byte(`{
		"fallback": "node-a",
		"rules": [
			{"domain": "ipinfo.io", "node": "node-b"},
			{"domain": "openai.com", "node": "node-c"}
		]
	}`), 0644)

	cfg, err := loadProxyRules(p)
	if err != nil {
		t.Fatalf("loadProxyRules: %v", err)
	}
	if cfg.fallback != "node-a" {
		t.Fatalf("fallback = %q, want node-a", cfg.fallback)
	}
	if got := cfg.rules["ipinfo.io"]; got != "node-b" {
		t.Fatalf("rules[ipinfo.io] = %q, want node-b", got)
	}
	if got := cfg.rules["openai.com"]; got != "node-c" {
		t.Fatalf("rules[openai.com] = %q, want node-c", got)
	}
	if len(cfg.rules) != 2 {
		t.Fatalf("rules count = %d, want 2", len(cfg.rules))
	}
}

func TestLoadProxyRulesMissingFile(t *testing.T) {
	cfg, err := loadProxyRules("/nonexistent/path/rules.json")
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(cfg.rules) != 0 {
		t.Fatalf("expected empty rules, got %d", len(cfg.rules))
	}
	if cfg.fallback != "" {
		t.Fatalf("expected empty fallback, got %q", cfg.fallback)
	}
}

func TestLoadProxyRulesBadJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rules.json")
	os.WriteFile(p, []byte(`{bad json`), 0644)

	_, err := loadProxyRules(p)
	if err == nil {
		t.Fatal("expected error on bad JSON, got nil")
	}
}

func TestLoadProxyRulesEmptyRules(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rules.json")
	os.WriteFile(p, []byte(`{"fallback": "node-a", "rules": []}`), 0644)

	cfg, err := loadProxyRules(p)
	if err != nil {
		t.Fatalf("loadProxyRules: %v", err)
	}
	if cfg.fallback != "node-a" {
		t.Fatalf("fallback = %q, want node-a", cfg.fallback)
	}
	if len(cfg.rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(cfg.rules))
	}
}

func TestLoadProxyRulesNoFallback(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rules.json")
	os.WriteFile(p, []byte(`{"rules": [{"domain":"a.com","node":"x"}]}`), 0644)

	cfg, err := loadProxyRules(p)
	if err != nil {
		t.Fatalf("loadProxyRules: %v", err)
	}
	if cfg.fallback != "" {
		t.Fatalf("expected empty fallback, got %q", cfg.fallback)
	}
	if cfg.rules["a.com"] != "x" {
		t.Fatalf("rules[a.com] = %q, want x", cfg.rules["a.com"])
	}
}
