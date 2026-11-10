package matching

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// noTitleForm 将 No.8 / No. 8 / no.8 等数字称号统一为 "No. N"。
var noTitleForm = regexp.MustCompile(`(?i)\bNo\.\s*(\d+)`)

// parallelSep 两侧有空白的 ASCII / 或全角 ／。
var parallelSep = regexp.MustCompile(`\s+[/／]\s+`)

// CleanTitleSurface 清理标题边界空白与单个终止句号，保留身份标点。
// 幂等；不删除 !、!!、;、冒号、连字符、内嵌点号或省略号。
// No.8 / No. 8 的数字称号比较形态统一为 No. 8。
func CleanTitleSurface(text string) string {
	s := strings.TrimSpace(text)
	if s == "" {
		return ""
	}
	s = noTitleForm.ReplaceAllString(s, "No. $1")
	s = stripTerminatingPeriod(s)
	return strings.TrimSpace(s)
}

func stripTerminatingPeriod(s string) string {
	s = strings.TrimRightFunc(s, unicode.IsSpace)
	if s == "" {
		return s
	}
	r, size := utf8.DecodeLastRuneInString(s)
	if r != '.' && r != '。' {
		return s
	}
	rest := strings.TrimRightFunc(s[:len(s)-size], unicode.IsSpace)
	if rest == "" {
		return ""
	}
	prev, _ := utf8.DecodeLastRuneInString(rest)
	// 省略号或连续点号保留
	if prev == '.' || prev == '。' || prev == '…' || prev == '⋯' {
		return s
	}
	return rest
}

// pipeSep ASCII | 或全角 ｜（两侧空白可有可无）。
var pipeSep = regexp.MustCompile(`\s*[|｜]\s*`)

// scriptBoundarySep CJK↔拉丁边界候选切点：空白或下划线连续段。
var scriptBoundarySep = regexp.MustCompile(`[\s_]+`)

// SplitParallelTitles 仅拆明确双语/并列标题，保持从左到右顺序，保留原语言文字。
//  1. | / ｜ 明确并列分隔（两段均非空才拆）；
//  2. 两侧有空白的 / 或全角 ／，且分隔后确有不同文字体系（一侧 CJK、另一侧拉丁）；
//  3. 空白或 _ 处的 CJK↔拉丁边界：一侧纯 CJK（≥2 字）、另一侧纯拉丁（≥3 字母），两侧剥去季号/媒体
//     标记后仍成立，且全串仅有唯一合格切点。
//
// Fate/Zero、Fate / Zero、らんま1/2、Ranma 1 / 2、某科学的超电磁炮 T 均不拆。
func SplitParallelTitles(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var out []string
	split := false
	pipeSegs, pipeSplit := splitOnPipe(text)
	split = split || pipeSplit
	for _, seg := range pipeSegs {
		slashSegs, slashSplit := splitOnParallelSlash(seg)
		split = split || slashSplit
		for _, s := range slashSegs {
			parts, ok := splitScriptBoundary(s)
			split = split || ok
			out = append(out, parts...)
		}
	}
	if !split {
		return []string{text}
	}
	return out
}

// splitOnPipe 按 | / ｜ 拆分；任一段为空时视为非并列分隔，不拆。
func splitOnPipe(text string) ([]string, bool) {
	if !pipeSep.MatchString(text) {
		return []string{text}, false
	}
	raw := pipeSep.Split(text, -1)
	segs := make([]string, 0, len(raw))
	for _, r := range raw {
		r = strings.TrimSpace(r)
		if r == "" {
			return []string{text}, false
		}
		segs = append(segs, r)
	}
	return segs, len(segs) > 1
}

// splitOnParallelSlash 现有 parallelSep 规则：仅在两侧文字体系不同时拆分。
func splitOnParallelSlash(text string) ([]string, bool) {
	idxs := parallelSep.FindAllStringIndex(text, -1)
	if len(idxs) == 0 {
		return []string{text}, false
	}

	segs := make([]string, 0, len(idxs)+1)
	prev := 0
	for _, ix := range idxs {
		segs = append(segs, text[prev:ix[0]])
		prev = ix[1]
	}
	segs = append(segs, text[prev:])

	var out []string
	var buf strings.Builder
	buf.WriteString(segs[0])
	splitCount := 0
	for i := 0; i < len(idxs); i++ {
		left := strings.TrimSpace(segs[i])
		right := strings.TrimSpace(segs[i+1])
		if isCJKLatinPair(left, right) {
			out = append(out, strings.TrimSpace(buf.String()))
			buf.Reset()
			buf.WriteString(segs[i+1])
			splitCount++
		} else {
			buf.WriteString(text[idxs[i][0]:idxs[i][1]])
			buf.WriteString(segs[i+1])
		}
	}
	out = append(out, strings.TrimSpace(buf.String()))
	if splitCount == 0 {
		return []string{text}, false
	}
	return out, true
}

// splitScriptBoundary 在唯一合格的空白/下划线切点处拆成纯 CJK 与纯拉丁两段。
func splitScriptBoundary(text string) ([]string, bool) {
	locs := scriptBoundarySep.FindAllStringIndex(text, -1)
	cut := -1
	for i, loc := range locs {
		left := strings.Trim(text[:loc[0]], " _")
		right := strings.Trim(text[loc[1]:], " _")
		if left == "" || right == "" {
			continue
		}
		if (isPureCJKSide(left) && isPureLatinSide(right)) || (isPureLatinSide(left) && isPureCJKSide(right)) {
			// 一侧只是季号/Part/Final/媒体标记（Re:Zero 第三季、迷宫饭 Season 2）时不拆
			if !hasTitleBaseAfterOrdinal(left) || !hasTitleBaseAfterOrdinal(right) {
				continue
			}
			if cut >= 0 {
				return []string{text}, false // 多个合格切点：歧义，不拆
			}
			cut = i
		}
	}
	if cut < 0 {
		return []string{text}, false
	}
	loc := locs[cut]
	return []string{strings.TrimSpace(strings.Trim(text[:loc[0]], " _")), strings.TrimSpace(strings.Trim(text[loc[1]:], " _"))}, true
}

// hasTitleBaseAfterOrdinal 剥去季号/Part/Final/媒体/年份标记后仍有足量文字。
func hasTitleBaseAfterOrdinal(side string) bool {
	base := AnalyzeTitle(side).Base
	return isPureCJKSide(base) || isPureLatinSide(base)
}

func isPureCJKSide(s string) bool {
	if hasLatinLetter(s) {
		return false
	}
	n := 0
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.In(r, unicode.Hiragana, unicode.Katakana) {
			n++
		}
	}
	return n >= 2
}

func isPureLatinSide(s string) bool {
	if hasCJK(s) {
		return false
	}
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) && (unicode.Is(unicode.Latin, r)) {
			n++
		}
	}
	return n >= 3
}

func isCJKLatinPair(a, b string) bool {
	aCJK, aLat := hasCJK(a), hasLatinLetter(a)
	bCJK, bLat := hasCJK(b), hasLatinLetter(b)
	return (aCJK && bLat) || (aLat && bCJK)
}

func hasLatinLetter(s string) bool {
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			return true
		}
		if unicode.Is(unicode.Latin, r) && unicode.IsLetter(r) {
			return true
		}
	}
	return false
}
