package main

import (
	"net/url"
	"testing"
)

func TestClashURLParse(t *testing.T) {
	cases := map[string]bool{
		"http://127.0.0.1:9090": true,
		"://bad":                false,
	}
	for raw, ok := range cases {
		_, err := url.Parse(raw)
		if ok && err != nil {
			t.Fatalf("expected %q to parse, got %v", raw, err)
		}
		if !ok && err == nil {
			t.Fatalf("expected %q to fail parsing", raw)
		}
	}
}

func TestResolveServiceNameNoPanic(t *testing.T) {
	// Runs only meaningfully on macOS with the VPNCheap app installed; must
	// never panic regardless of environment.
	name := resolveServiceName(nil)
	if name == "" {
		t.Log("no vpncheap service found (non-macOS or not installed)")
	}
}
