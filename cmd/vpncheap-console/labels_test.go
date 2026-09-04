package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCfg(t *testing.T, outbounds string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.json")
	os.WriteFile(p, []byte(`{"outbounds":[`+outbounds+`]}`), 0644)
	return p
}

func TestLoadNodeTagsOrdered(t *testing.T) {
	p := writeCfg(t,
		`{"type":"hysteria2","tag":"HK-香港1"},`+
			`{"type":"selector","tag":"proxy","outbounds":[]},`+
			`{"type":"anytls","tag":"JP-日本1"},`+
			`{"type":"direct","tag":"direct"},`+
			`{"type":"urltest","tag":"auto"}`)
	tags := loadNodeTags(p)
	want := []string{"HK-香港1", "JP-日本1"}
	if len(tags) != len(want) {
		t.Fatalf("got %d tags want %d: %v", len(tags), len(want), tags)
	}
	for i := range want {
		if tags[i] != want[i] {
			t.Fatalf("tags[%d]=%q want %q", i, tags[i], want[i])
		}
	}
}

func TestLoadNodeTagsMissingFile(t *testing.T) {
	if tags := loadNodeTags("/nonexistent/never.json"); tags != nil {
		t.Fatalf("missing file should return nil, got %v", tags)
	}
}

func TestLoadNodeTagsBadJSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(p, []byte("not json"), 0644)
	if tags := loadNodeTags(p); tags != nil {
		t.Fatalf("bad json should return nil, got %v", tags)
	}
}

// labelsHandler with a fake clash API returning proxy members in a known order.
func TestLabelsHandlerMapsPositionally(t *testing.T) {
	// fake clash upstream
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"all": []string{"xboard_aaa", "xboard_bbb", "direct"}})
	}))
	defer up.Close()

	cfgPath := writeCfg(t,
		`{"type":"hysteria2","tag":"HK-香港1"},`+
			`{"type":"anytls","tag":"JP-日本1"}`)

	// point labelFiles at the temp cfg only
	old := labelFiles
	labelFiles = []string{cfgPath}
	defer func() { labelFiles = old }()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/labels", nil)
	labelsHandler(nil, up.URL).ServeHTTP(rec, req)

	var resp struct {
		Mapping map[string]string `json:"mapping"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, rec.Body.String())
	}
	if resp.Mapping["xboard_aaa"] != "HK-香港1" {
		t.Fatalf("xboard_aaa mapped to %q want HK-香港1", resp.Mapping["xboard_aaa"])
	}
	if resp.Mapping["xboard_bbb"] != "JP-日本1" {
		t.Fatalf("xboard_bbb mapped to %q want JP-日本1", resp.Mapping["xboard_bbb"])
	}
	if _, ok := resp.Mapping["direct"]; ok {
		t.Fatalf("direct must not be in mapping")
	}
}

// If the clash API is down, labelsHandler returns an empty mapping (UI falls back).
func TestLabelsHandlerClashDown(t *testing.T) {
	cfgPath := writeCfg(t, `{"type":"hysteria2","tag":"HK-香港1"}`)
	old := labelFiles
	labelFiles = []string{cfgPath}
	defer func() { labelFiles = old }()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/labels", nil)
	labelsHandler(nil, "http://127.0.0.1:1").ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"mapping":{}`) {
		t.Fatalf("expected empty mapping, got %s", rec.Body.String())
	}
}
