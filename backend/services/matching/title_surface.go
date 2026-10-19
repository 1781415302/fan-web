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

// SplitParallelTitles 仅拆明确双语标题，保持从左到右顺序。
// 识别两侧有空白的 / 或全角 ／，且分隔后确有不同文字体系（一侧 CJK、另一侧拉丁）。
// Fate/Zero、Fate / Zero、らんま1/2、Ranma 1 / 2 均不拆。
func SplitParallelTitles(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	idxs := parallelSep.FindAllStringIndex(text, -1)
	if len(idxs) == 0 {
		return []string{text}
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
		return []string{text}
	}
	return out
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
