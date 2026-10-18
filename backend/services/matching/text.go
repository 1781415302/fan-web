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
		m := make(map[rune]rune, 4096)
		for _, line := range strings.Split(t2sData, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			tr, _ := utf8.DecodeRuneInString(parts[0])
			sr, _ := utf8.DecodeRuneInString(parts[1])
			if tr == utf8.RuneError || sr == utf8.RuneError {
				continue
			}
			m[tr] = sr
		}
		t2sMap = m
	})
	return t2sMap
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
