package matching

import "strings"

// TitlesShareStrongIdentity 判断两标题是否共享严格/常规/catalog 级作品身份。
// 复用 compareNames，明确排除模糊与 Loose，避免为改名另写宽松规则。
func TitlesShareStrongIdentity(a, b string, catalog Catalog) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	qa := identityTitle{text: a, source: SourceFile, facts: AnalyzeTitle(a), forms: Normalize(a)}
	sb := identityTitle{text: b, source: SourceFile, facts: AnalyzeTitle(b), forms: Normalize(b)}
	h := compareNames(qa, sb, catalog)
	switch h.kind {
	case NameFullStrict, NameFullNormal, NameBaseStrict, NameBaseNormal, NameCatalog:
		return true
	default:
		return false
	}
}

// FileEvidenceShareWork 判断两份文件证据是否属于同一作品（标题身份）。
// 任一侧无可用标题时无法证明，返回 false。
func FileEvidenceShareWork(a, b FileEvidence, catalog Catalog) bool {
	aTitles := evidenceTitleTexts(a)
	bTitles := evidenceTitleTexts(b)
	if len(aTitles) == 0 || len(bTitles) == 0 {
		return false
	}
	for _, at := range aTitles {
		for _, bt := range bTitles {
			if TitlesShareStrongIdentity(at, bt, catalog) {
				return true
			}
		}
	}
	return false
}

func evidenceTitleTexts(ev FileEvidence) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		key := TitleKey(text)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, text)
		// 剥季后的 base 也参与比较（与评分路径一致）
		base := strings.TrimSpace(AnalyzeTitle(text).Base)
		if base != "" && TitleKey(base) != key {
			bk := TitleKey(base)
			if _, ok := seen[bk]; !ok {
				seen[bk] = struct{}{}
				out = append(out, base)
			}
		}
	}
	for _, t := range ev.Titles {
		add(t.Text)
	}
	return out
}
