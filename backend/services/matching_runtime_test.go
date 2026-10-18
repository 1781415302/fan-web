package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"fan-web/services/matching"
)

func TestMatchingRuntimeLoadsBuiltinCatalog(t *testing.T) {
	rt, err := NewMatchingRuntime(NewBangumiService(), matching.EngineV2, "")
	if err != nil {
		t.Fatalf("NewMatchingRuntime: %v", err)
	}
	if rt.Engine() != matching.EngineV2 {
		t.Fatalf("engine=%q want v2", rt.Engine())
	}
	expanded := rt.Catalog().Expand("The Daily Life of the Immortal King")
	if len(expanded) == 0 {
		t.Fatal("builtin catalog should expand 仙王 English alias")
	}
	sess := rt.NewSession(context.Background())
	if sess == nil || sess.provider == nil {
		t.Fatal("NewSession should inject provider when bangumi set")
	}
}

func TestMatchingRuntimeRejectsMissingAliasFile(t *testing.T) {
	_, err := NewMatchingRuntime(nil, matching.EngineV2, filepath.Join(t.TempDir(), "missing.json"))
	if err == nil {
		t.Fatal("want error for missing alias file")
	}
}

func TestMatchingRuntimeRejectsAliasDirectory(t *testing.T) {
	dir := t.TempDir()
	_, err := NewMatchingRuntime(nil, matching.EngineV2, dir)
	if err == nil {
		t.Fatal("want error when alias path is directory")
	}
}

func TestMatchingRuntimeRejectsInvalidAliasJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{not-json`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NewMatchingRuntime(nil, matching.EngineV2, path)
	if err == nil {
		t.Fatal("want error for invalid alias JSON")
	}
}

func TestMatchingRuntimeOverrideCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "override.json")
	raw := []byte(`{
		"version": 1,
		"families": [
			{"key": "demo", "canonical": "演示番", "aliases": ["Demo Show"]}
		]
	}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := NewMatchingRuntime(nil, matching.EngineLegacy, path)
	if err != nil {
		t.Fatalf("NewMatchingRuntime: %v", err)
	}
	if len(rt.Catalog().Expand("Demo Show")) == 0 {
		t.Fatal("override alias not indexed")
	}
}

func TestMatchingRuntimeMatchEngineFromEnv(t *testing.T) {
	cases := []struct {
		in   string
		want matching.Engine
	}{
		{"", matching.EngineV2},
		{"v2", matching.EngineV2},
		{"V2", matching.EngineV2},
		{"legacy", matching.EngineLegacy},
		{"LEGACY", matching.EngineLegacy},
		{"weird", matching.EngineLegacy},
	}
	for _, tc := range cases {
		if got := MatchEngineFromEnv(tc.in); got != tc.want {
			t.Fatalf("MatchEngineFromEnv(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
	if got := (*MatchingRuntime)(nil).Engine(); got != matching.EngineLegacy {
		t.Fatalf("nil runtime Engine=%q", got)
	}
}
