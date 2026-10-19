package matching

import (
	_ "embed"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

//go:embed data/t2s.txt
var t2sData string

//go:embed data/OPENCC-LICENSE.txt
var openCCLicense string

var (
	t2sOnce sync.Once
	t2sMap  map[rune]rune
)

func loadT2S() map[rune]rune {
	t2sOnce.Do(func() {
		t2sMap = parseT2SMapping(t2sData)
	})
	return t2sMap
}

// parseT2SMapping 解析单字繁简表。
// 每行：一个 Unicode scalar 源字 + 一个或多个单字目标。
// 单一（或重复相同）目标 → 折叠到该目标；多个不同目标 → 保留源字（不入映射）。
// 空行/注释跳过；多字源或目标的非法行不入映射。
func parseT2SMapping(data string) map[rune]rune {
	m := make(map[rune]rune, 4096)
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		src := []rune(parts[0])
		if len(src) != 1 || src[0] == utf8.RuneError {
			continue
		}
		var targets []rune
		ok := true
		for _, p := range parts[1:] {
			tr := []rune(p)
			if len(tr) != 1 || tr[0] == utf8.RuneError {
				ok = false
				break
			}
			targets = append(targets, tr[0])
		}
		if !ok || len(targets) == 0 {
			continue
		}
		first := targets[0]
		ambiguous := false
		for _, t := range targets[1:] {
			if t != first {
				ambiguous = true
				break
			}
		}
		if ambiguous {
			// 多目标歧义：保留源字，不写入映射。
			continue
		}
		m[src[0]] = first
	}
	return m
}

// Normalize 返回原文的严格/常规/宽松比较形态；保留身份区分信息。
// Strict：NFKC + 全半角折叠 + 繁简（不改大小写/区分标点）。
// Normal：Strict + 拉丁小写 + 空白折叠。
// Loose：Normal 上去除空白，假名平片假名折叠，弱比较用。
func Normalize(text string) TextForms {
	strict := toStrict(text)
	normal := toNormal(strict)
	loose := toLoose(normal)
	return TextForms{Strict: strict, Normal: normal, Loose: loose}
}

// TitleKey 稳定比较键，不用于覆盖原始字符串。
func TitleKey(text string) string {
	return Normalize(text).Normal
}

func toStrict(text string) string {
	if text == "" {
		return ""
	}
	s := norm.NFKC.String(text)
	s = foldWidthAndT2S(s)
	return s
}

func toNormal(strict string) string {
	if strict == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(strict))
	prevSpace := false
	for _, r := range strict {
		if unicode.IsSpace(r) {
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = false
		if r >= 'A' && r <= 'Z' {
			r = r - 'A' + 'a'
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func toLoose(normal string) string {
	if normal == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(normal))
	for _, r := range normal {
		if unicode.IsSpace(r) {
			continue
		}
		// 平假名 → 片假名（弱折叠）
		if r >= 0x3041 && r <= 0x3096 {
			r = r + 0x60
		}
		b.WriteRune(r)
	}
	return b.String()
}

func foldWidthAndT2S(s string) string {
	m := loadT2S()
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		r = foldWidthRune(r)
		if simp, ok := m[r]; ok {
			r = simp
		}
		b.WriteRune(r)
	}
	return b.String()
}

func foldWidthRune(r rune) rune {
	switch {
	case r >= '０' && r <= '９':
		return r - '０' + '0'
	case r >= 'Ａ' && r <= 'Ｚ':
		return r - 'Ａ' + 'A'
	case r >= 'ａ' && r <= 'ｚ':
		return r - 'ａ' + 'a'
	case r == '　':
		return ' '
	case r >= 0xFF01 && r <= 0xFF5E:
		// 全角标点/符号 → 半角
		return r - 0xFEE0
	default:
		return r
	}
}

// openCCLicenseText 暴露许可证文本供测试断言非空（生产不依赖）。
func openCCLicenseText() string { return openCCLicense }
