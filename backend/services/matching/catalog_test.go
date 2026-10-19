package matching

import (
	"strings"
	"testing"
)

func TestCatalogBuiltinXianWangExpand(t *testing.T) {
	cat, err := LoadCatalog(BuiltinTitleAliasesJSON, nil)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if cat.Empty() {
		t.Fatal("builtin catalog empty")
	}

	inputs := []string{
		"仙王的日常生活",
		"The Daily Life of the Immortal King",
		"Xian Wang De Ri Chang Sheng Huo",
		"仙王的日常生活 第4季", // AnalyzeTitle 抽 base，保留季号在 Ordinal
	}
	for _, in := range inputs {
		got := cat.Expand(in)
		if len(got) < 2 {
			t.Fatalf("Expand(%q) got %#v want family titles", in, got)
		}
		var hasCN, hasEN, hasPY bool
		for _, ti := range got {
			if ti.Source != SourceCatalog {
				t.Fatalf("Expand(%q) source=%s want catalog", in, ti.Source)
			}
			k := TitleKey(ti.Text)
			switch {
			case strings.Contains(k, "仙王"):
				hasCN = true
			case strings.Contains(k, "immortal king"):
				hasEN = true
			case strings.Contains(k, "xian wang"):
				hasPY = true
			}
		}
		if !hasCN || !hasEN || !hasPY {
			t.Fatalf("Expand(%q) missing family members: %#v (cn=%v en=%v py=%v)", in, got, hasCN, hasEN, hasPY)
		}
	}
}

func TestCatalogExpandPreservesSeasonInAnalyze(t *testing.T) {
	cat, err := LoadCatalog(BuiltinTitleAliasesJSON, nil)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	in := "仙王的日常生活 第4季"
	facts := AnalyzeTitle(in)
	if facts.Ordinal.Season != 4 {
		t.Fatalf("season stripped before expand: %+v", facts)
	}
	got := cat.Expand(in)
	if len(got) == 0 {
		t.Fatal("expand failed for titled season form")
	}
	// 扩展结果是系列名，不含强制季号；季号由 Query.Ordinal 保留
	for _, ti := range got {
		if strings.Contains(ti.Text, "第4季") {
			t.Fatalf("catalog title should be series base, got %q", ti.Text)
		}
	}
	if facts.Ordinal.Season != 4 {
		t.Fatal("season lost after expand path")
	}
}

func TestCatalogRejectInvalidJSON(t *testing.T) {
	_, err := LoadCatalog([]byte(`{not-json`), nil)
	if err == nil {
		t.Fatal("want invalid JSON error")
	}
	_, err = LoadCatalog([]byte(`{"version":2,"families":[]}`), nil)
	if err == nil {
		t.Fatal("want unsupported version error")
	}
	_, err = LoadCatalog([]byte(`{"version":1,"families":[{"key":"","canonical":"A","aliases":[]}]}`), nil)
	if err == nil {
		t.Fatal("want empty key error")
	}
}

func TestCatalogRejectCrossFamilyConflict(t *testing.T) {
	builtin := []byte(`{
		"version": 1,
		"families": [
			{"key": "a", "canonical": "Alpha Show", "aliases": ["Shared Alias"]},
			{"key": "b", "canonical": "Beta Show", "aliases": ["Shared Alias"]}
		]
	}`)
	_, err := LoadCatalog(builtin, nil)
	if err == nil {
		t.Fatal("want cross-family alias conflict")
	}
	if !strings.Contains(err.Error(), "maps to both") && !strings.Contains(err.Error(), "Shared") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCatalogOverrideReplacesFamily(t *testing.T) {
	builtin := []byte(`{
		"version": 1,
		"families": [
			{"key": "demo", "canonical": "旧名", "aliases": ["Old Alias"]}
		]
	}`)
	override := []byte(`{
		"version": 1,
		"families": [
			{"key": "demo", "canonical": "新名", "aliases": ["New Alias"]}
		]
	}`)
	cat, err := LoadCatalog(builtin, override)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	got := cat.Expand("New Alias")
	if len(got) == 0 {
		t.Fatal("override alias not indexed")
	}
	found := false
	for _, ti := range got {
		if ti.Text == "新名" {
			found = true
		}
		if ti.Text == "旧名" || TitleKey(ti.Text) == TitleKey("Old Alias") {
			t.Fatalf("old family retained: %#v", got)
		}
	}
	if !found {
		t.Fatalf("canonical not replaced: %#v", got)
	}
	if len(cat.Expand("Old Alias")) != 0 {
		t.Fatal("old alias should not hit after replace")
	}
}

func TestCatalogEmptyOK(t *testing.T) {
	cat, err := LoadCatalog(nil, nil)
	if err != nil {
		t.Fatalf("empty catalog: %v", err)
	}
	if !cat.Empty() {
		t.Fatal("want empty")
	}
	if cat.Expand("anything") != nil {
		t.Fatal("empty expand should be nil")
	}
}

func TestCatalogExactBaseOnlyNoSubstring(t *testing.T) {
	cat, err := LoadCatalog(BuiltinTitleAliasesJSON, nil)
	if err != nil {
		t.Fatal(err)
	}
	// 子串不应命中
	if got := cat.Expand("日常生活"); len(got) != 0 {
		t.Fatalf("substring should not expand: %#v", got)
	}
	if got := cat.Expand("Immortal"); len(got) != 0 {
		t.Fatalf("partial english should not expand: %#v", got)
	}
}
