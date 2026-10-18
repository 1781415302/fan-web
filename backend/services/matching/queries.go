package matching

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// 查询调度参数阈值（集中声明；调参属 S6，不得为单例改数）。
const (
	queryTermBudget = 6
	maxShortenSteps = 2
	minShortenWords = 2
)

// BuildQueries 生成去重、有优先级的查询词，最多 queryTermBudget 条。
// 顺序：catalog 中日文 canonical → 信息最多的文件标题 → 其他语言标题 →
// 带明确季号的查询 →（无文件标题时）目录标题 → 拉丁多词缩短（计入预算）。
func BuildQueries(q Query, catalog Catalog) []QueryTerm {
	out := make([]QueryTerm, 0, queryTermBudget)
	seen := make(map[string]struct{}, queryTermBudget)

	add := func(text string, src Source, shortened bool) {
		if len(out) >= queryTermBudget {
			return
		}
		text = strings.TrimSpace(text)
		if !isUsefulQueryText(text) {
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
		out = append(out, QueryTerm{Text: text, Source: src, Shortened: shortened})
	}

	fileTitles, dirTitles := splitQueryTitles(q)

	expandInputs := fileTitles
	if len(expandInputs) == 0 {
		expandInputs = dirTitles
	}

	// 1. 可靠 catalog 命中的中文/日文 canonical（及其它 CJK 扩展名）
	for _, t := range expandInputs {
		for _, et := range catalog.Expand(t.Text) {
			if queryHasCJK(et.Text) {
				add(et.Text, SourceCatalog, false)
			}
		}
	}

	// 2. 信息最多的文件标题
	primary := pickMostInformativeTitle(fileTitles)
	primaryBase := titleBase(primary.Text)
	if primaryBase != "" {
		add(primaryBase, SourceFile, false)
	}

	// 3. 其他语言 / 其余文件标题
	for _, t := range fileTitles {
		base := titleBase(t.Text)
		add(base, SourceFile, false)
	}

	// 4. 带明确季号的查询
	if q.Ordinal.Season > 0 {
		season := q.Ordinal.Season
		for _, t := range expandInputs {
			for _, et := range catalog.Expand(t.Text) {
				add(withSeasonQualifier(et.Text, season), SourceCatalog, false)
			}
		}
		for _, t := range fileTitles {
			add(withSeasonQualifier(titleBase(t.Text), season), SourceFile, false)
		}
	}

	// 5. 目录标题：仅无文件标题时用于检索
	if len(fileTitles) == 0 {
		for _, t := range dirTitles {
			base := titleBase(t.Text)
			add(base, SourceDirectory, false)
			if q.Ordinal.Season > 0 {
				add(withSeasonQualifier(base, q.Ordinal.Season), SourceDirectory, false)
			}
		}
	}

	// 6. 拉丁多词缩短：最多 2 次，至少保留 2 词，计入总预算
	if primaryBase != "" && isLatinMultiWord(primaryBase) {
		words := strings.Fields(primaryBase)
		for step := 1; step <= maxShortenSteps && len(words)-step >= minShortenWords; step++ {
			short := strings.Join(words[:len(words)-step], " ")
			add(short, SourceFile, true)
		}
	}

	return out
}

func splitQueryTitles(q Query) (fileTitles, dirTitles []Title) {
	for _, t := range q.Titles {
		switch t.Source {
		case SourceDirectory:
			dirTitles = append(dirTitles, t)
		case SourceCatalog:
			// catalog 扩展结果不应再作为输入标题；若出现则忽略
		default:
			fileTitles = append(fileTitles, t)
		}
	}
	if !q.HasFileTitle {
		// 契约：无文件标题时目录可检索；文件切片置空以免误用
		if len(fileTitles) > 0 && len(dirTitles) == 0 {
			// Titles 可能未标 SourceDirectory；HasFileTitle=false 时全部当目录线索
			dirTitles = append(dirTitles, fileTitles...)
			fileTitles = nil
		} else if len(fileTitles) > 0 {
			dirTitles = append(dirTitles, fileTitles...)
			fileTitles = nil
		}
	}
	return fileTitles, dirTitles
}

func titleBase(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	base := strings.TrimSpace(AnalyzeTitle(text).Base)
	if base == "" {
		return text
	}
	return base
}

func pickMostInformativeTitle(titles []Title) Title {
	best := Title{}
	bestScore := -1
	for _, t := range titles {
		base := titleBase(t.Text)
		score := utf8.RuneCountInString(base) + len(strings.Fields(base))
		if score > bestScore {
			bestScore = score
			best = Title{Text: base, Source: t.Source}
		}
	}
	return best
}

func withSeasonQualifier(base string, season int) string {
	base = strings.TrimSpace(base)
	if base == "" || season <= 0 {
		return base
	}
	if queryHasCJK(base) {
		return fmt.Sprintf("%s 第%d季", base, season)
	}
	return fmt.Sprintf("%s %d", base, season)
}

func isLatinMultiWord(text string) bool {
	words := strings.Fields(strings.TrimSpace(text))
	if len(words) < minShortenWords+1 {
		// 至少 3 词才有缩短空间（缩短后仍 ≥2）
		return false
	}
	for _, w := range words {
		for _, r := range w {
			if unicode.Is(unicode.Han, r) || unicode.In(r, unicode.Hiragana, unicode.Katakana) {
				return false
			}
		}
	}
	return true
}

func queryHasCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.In(r, unicode.Hiragana, unicode.Katakana) {
			return true
		}
	}
	return false
}

var categoryOnlyQueries = map[string]struct{}{
	"国漫": {}, "日漫": {}, "动漫": {}, "动画": {}, "動畫": {},
	"anime": {}, "dongman": {},
}

func isUsefulQueryText(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	key := TitleKey(text)
	if key == "" {
		return false
	}
	if _, ok := categoryOnlyQueries[key]; ok {
		return false
	}
	// 过短：纯拉丁单字符
	if utf8.RuneCountInString(key) < 2 {
		onlyLatin := true
		for _, r := range key {
			if r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsDigit(r)) {
				onlyLatin = false
				break
			}
		}
		if onlyLatin {
			return false
		}
	}
	return true
}
