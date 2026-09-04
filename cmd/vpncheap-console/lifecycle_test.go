package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPidfileWriteReadRemove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")
	if readPidfile(path) != 0 {
		t.Fatalf("readPidfile on missing file should return 0")
	}
	if err := writePidfile(path); err != nil {
		t.Fatalf("writePidfile: %v", err)
	}
	pid := readPidfile(path)
	if pid != os.Getpid() {
		t.Fatalf("readPidfile got %d want %d", pid, os.Getpid())
	}
	removePidfile(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("pidfile should be removed, got %v", err)
	}
}

func TestPidfileInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.pid")
	os.WriteFile(path, []byte("notanumber"), 0644)
	if readPidfile(path) != 0 {
		t.Fatalf("readPidfile on non-numeric content should return 0")
	}
}

func TestPhaseSetGet(t *testing.T) {
	setPhase("launching", "starting app")
	p, msg := getPhase()
	if p != "launching" || msg != "starting app" {
		t.Fatalf("got %q/%q want launching/starting app", p, msg)
	}
	setPhase("ready", "ok")
	p, msg = getPhase()
	if p != "ready" || msg != "ok" {
		t.Fatalf("got %q/%q want ready/ok", p, msg)
	}
	setPhase("launching", "init") // restore default
}

func TestHealthHandlerJSON(t *testing.T) {
	setPhase("degraded", "timeout")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	healthHandler().ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{`"phase":"degraded"`, `"detail":"timeout"`} {
		if !containsStr(body, want) {
			t.Fatalf("health body %q missing %q", body, want)
		}
	}
	setPhase("launching", "init")
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
