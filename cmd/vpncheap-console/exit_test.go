package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExitHandlerProbeSuccess(t *testing.T) {
	// Fake geolocation server.
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ip":"1.2.3.4","city":"Tokyo","region":"Tokyo","country":"JP","org":"AS Test"}`))
	}))
	defer probe.Close()

	old := exitProbeURL
	exitProbeURL = probe.URL
	defer func() { exitProbeURL = old }()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/exit", nil)
	exitHandler(nil).ServeHTTP(rec, req)

	var d map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if d["ip"] != "1.2.3.4" || d["country"] != "JP" || d["city"] != "Tokyo" {
		t.Fatalf("unexpected probe payload: %v", d)
	}
	if _, ok := d["tunnel"]; !ok {
		t.Fatalf("expected tunnel field, got %v", d)
	}
	if _, ok := d["error"]; ok {
		t.Fatalf("unexpected error field: %v", d)
	}
}

func TestExitHandlerProbeFailure(t *testing.T) {
	old := exitProbeURL
	exitProbeURL = "http://127.0.0.1:1"
	defer func() { exitProbeURL = old }()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/exit", nil)
	exitHandler(nil).ServeHTTP(rec, req)

	var d map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if _, ok := d["error"]; !ok {
		t.Fatalf("expected error field on probe failure, got %v", d)
	}
}
