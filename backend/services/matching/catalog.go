package matching

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	_ "embed"
)

//go:embed data/title_aliases.json
var BuiltinTitleAliasesJSON []byte

type catalogDocument struct {
	Version  int           `json:"version"`
	Families []AliasFamily `json:"families"`
}

// LoadCatalog 验证并合并内置与可选覆盖的系列映射。
// builtinJSON / overrideJSON 为空切片或 nil 表示该层无内容；两者皆空则返回空 Catalog。
// 同 key 时 override 整族替换；合并后做全局别名冲突检查。
func LoadCatalog(builtinJSON, overrideJSON []byte) (Catalog, error) {
	builtin, err := parseCatalogDocument(builtinJSON, "builtin")
	if err != nil {
		return Catalog{}, err
	}
	override, err := parseCatalogDocument(overrideJSON, "override")
	if err != nil {
		return Catalog{}, err
	}

	merged := make(map[string]AliasFamily, len(builtin)+len(override))
	order := make([]string, 0, len(builtin)+len(override))
	for _, fam := range builtin {
		if _, ok := merged[fam.Key]; !ok {
			order = append(order, fam.Key)
		}
		merged[fam.Key] = fam
	}
	for _, fam := range override {
		if _, ok := merged[fam.Key]; !ok {
			order = append(order, fam.Key)
		}
		merged[fam.Key] = fam // 同 key 整族替换
	}

	families := make([]AliasFamily, 0, len(order))
	for _, key := range order {
		families = append(families, merged[key])
	}
	return indexFamilies(families)
}

func parseCatalogDocument(raw []byte, label string) ([]AliasFamily, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil
	}
	var doc catalogDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("catalog %s: invalid JSON: %w", label, err)
	}
	if doc.Version != 1 {
		return nil, fmt.Errorf("catalog %s: unsupported version %d (want 1)", label, doc.Version)
	}
	seenKey := make(map[string]struct{}, len(doc.Families))
	out := make([]AliasFamily, 0, len(doc.Families))
	for i, fam := range doc.Families {
		fam.Key = strings.TrimSpace(fam.Key)
		fam.Canonical = strings.TrimSpace(fam.Canonical)
		if fam.Key == "" {
			return nil, fmt.Errorf("catalog %s: family[%d] empty key", label, i)
		}
		if fam.Canonical == "" {
			return nil, fmt.Errorf("catalog %s: family %q empty canonical", label, fam.Key)
		}
		if _, ok := seenKey[fam.Key]; ok {
			return nil, fmt.Errorf("catalog %s: duplicate family key %q", label, fam.Key)
		}
		seenKey[fam.Key] = struct{}{}
		aliases := make([]string, 0, len(fam.Aliases))
		for j, a := range fam.Aliases {
			a = strings.TrimSpace(a)
			if a == "" {
				return nil, fmt.Errorf("catalog %s: family %q alias[%d] empty", label, fam.Key, j)
			}
			aliases = append(aliases, a)
		}
		fam.Aliases = aliases
		out = append(out, fam)
	}
	return out, nil
}

func indexFamilies(families []AliasFamily) (Catalog, error) {
	c := Catalog{
		families: families,
		index:    make(map[string]int, len(families)*4),
	}
	for i, fam := range families {
		for _, title := range familyTitles(fam) {
			forms := Normalize(title)
			for _, key := range []string{forms.Strict, forms.Normal} {
				if key == "" {
					continue
				}
				if prev, ok := c.index[key]; ok {
					if c.families[prev].Key != fam.Key {
						return Catalog{}, fmt.Errorf(
							"catalog: alias %q maps to both %q and %q",
							title, c.families[prev].Key, fam.Key,
						)
					}
					continue
				}
				c.index[key] = i
			}
		}
	}
	return c, nil
}

func familyTitles(fam AliasFamily) []string {
	out := make([]string, 0, 1+len(fam.Aliases))
	out = append(out, fam.Canonical)
	out = append(out, fam.Aliases...)
	return out
}

// Expand 只对完整系列 base 的精确别名命中扩展。
// 先 AnalyzeTitle 抽 Ordinal/Base，再对 base 做严格/常规键精确命中；
// 季号留在 TitleFacts.Ordinal / 调用方 Query.Ordinal，不丢弃。
func (c Catalog) Expand(title string) []Title {
	if len(c.families) == 0 || len(c.index) == 0 {
		return nil
	}
	raw := strings.TrimSpace(title)
	if raw == "" {
		return nil
	}
	facts := AnalyzeTitle(raw)
	base := strings.TrimSpace(facts.Base)
	if base == "" {
		base = raw
	}
	idx, ok := c.lookupBase(base)
	if !ok {
		return nil
	}
	fam := c.families[idx]
	titles := familyTitles(fam)
	out := make([]Title, 0, len(titles))
	seen := make(map[string]struct{}, len(titles))
	for _, text := range titles {
		key := TitleKey(text)
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, Title{Text: text, Source: SourceCatalog})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (c Catalog) lookupBase(base string) (int, bool) {
	forms := Normalize(base)
	for _, key := range []string{forms.Strict, forms.Normal} {
		if key == "" {
			continue
		}
		if idx, ok := c.index[key]; ok {
			return idx, true
		}
	}
	return 0, false
}

// Empty reports whether the catalog has no families.
func (c Catalog) Empty() bool {
	return len(c.families) == 0
}
