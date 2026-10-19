package matching

import (
	"strings"
	"sync"
	"testing"
)

func TestTextNormalizeForms(t *testing.T) {
	forms := Normalize("　Ｋ－ＯＮ！！　")
	if forms.Strict == "" || forms.Normal == "" || forms.Loose == "" {
		t.Fatalf("empty forms: %+v", forms)
	}
	if !strings.Contains(forms.Strict, "!!") && !strings.Contains(forms.Strict, "！") {
		// fullwidth bang folded to half
		if !strings.Contains(forms.Strict, "K-ON") && !strings.Contains(forms.Strict, "K-ON!!") {
			t.Fatalf("strict lost identity: %q", forms.Strict)
		}
	}
	if forms.Normal != strings.ToLower(forms.Normal) {
		t.Fatalf("normal should be lowercased latin: %q", forms.Normal)
	}
	if strings.Contains(forms.Loose, " ") {
		t.Fatalf("loose should drop spaces: %q", forms.Loose)
	}
}

func TestTextTraditionalToSimplified(t *testing.T) {
	forms := Normalize("國漫動畫")
	if !strings.Contains(forms.Strict, "国") || !strings.Contains(forms.Strict, "动画") {
		t.Fatalf("t2s failed: strict=%q", forms.Strict)
	}
}

func TestTextTitleKeyStable(t *testing.T) {
	a := TitleKey("Fate/Zero")
	b := TitleKey("  Fate/Zero  ")
	if a == "" || a != b {
		t.Fatalf("TitleKey unstable: %q vs %q", a, b)
	}
}

func TestTextOpenCCLicenseEmbedded(t *testing.T) {
	lic := openCCLicenseText()
	if !strings.Contains(lic, "Apache License") {
		t.Fatalf("OPENCC license missing Apache text")
	}
	if !strings.Contains(lic, "OpenCC") && !strings.Contains(lic, "Open Chinese Convert") {
		// provenance note mentions OpenCC
		if !strings.Contains(lic, "Provenance") {
			t.Fatalf("license provenance missing")
		}
	}
}

func TestTextKanaLooseFold(t *testing.T) {
	hira := Normalize("ひらがな").Loose
	kata := Normalize("ヒラガナ").Loose
	if hira == "" || hira != kata {
		t.Fatalf("kana loose fold: %q vs %q", hira, kata)
	}
}

func TestRecallT2SAmbiguousKeepsSource(t *testing.T) {
	// 乾 干 乾：多目标歧义，乾 不得折叠为 干
	qian := Normalize("乾坤").Strict
	gan := Normalize("干坤").Strict
	if qian == gan {
		t.Fatalf("ambiguous 乾 must not map to 干: %q == %q", qian, gan)
	}
	if !strings.Contains(qian, "乾") {
		t.Fatalf("乾坤 Strict should keep 乾: %q", qian)
	}
	if !strings.Contains(gan, "干") {
		t.Fatalf("干坤 Strict should keep 干: %q", gan)
	}
}

func TestRecallT2STaiwanEquivalence(t *testing.T) {
	a := Normalize("臺灣").Strict
	b := Normalize("台湾").Strict
	if a == "" || a != b {
		t.Fatalf("臺灣/台湾 should share Strict: %q vs %q", a, b)
	}
	if !strings.Contains(a, "台湾") && !strings.Contains(a, "台") {
		// 臺→台, 灣→湾
		t.Fatalf("expected simplified form, got %q", a)
	}
}

func TestRecallT2SParseRules(t *testing.T) {
	data := strings.Join([]string{
		"# comment",
		"",
		"國 国",    // 单目标
		"乾 干 乾",  // 多目标歧义 → 不入映射
		"甲 乙 乙",  // 重复相同目标 → 折叠
		"多字源 简",  // 非法多字源
		"源 多字目标", // 非法多字目标
		"好 好",    // 单目标恒等也可入图
	}, "\n")
	m := parseT2SMapping(data)

	if got, ok := m['國']; !ok || got != '国' {
		t.Fatalf("single target 國→国: ok=%v got=%q", ok, got)
	}
	if _, ok := m['乾']; ok {
		t.Fatalf("ambiguous 乾 must not enter map")
	}
	if got, ok := m['甲']; !ok || got != '乙' {
		t.Fatalf("repeated same target 甲→乙: ok=%v got=%q", ok, got)
	}
	// 非法行
	for _, r := range []rune{'多', '源'} {
		if _, ok := m[r]; ok {
			t.Fatalf("illegal line rune %q should not map", string(r))
		}
	}
	if got, ok := m['好']; !ok || got != '好' {
		t.Fatalf("identity single target: ok=%v got=%q", ok, got)
	}
}

func TestRecallT2SConcurrentInit(t *testing.T) {
	var wg sync.WaitGroup
	errs := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a := Normalize("臺灣").Strict
			b := Normalize("台湾").Strict
			if a != b || a == "" {
				errs <- "taiwan mismatch"
			}
			q := Normalize("乾坤").Strict
			g := Normalize("干坤").Strict
			if q == g {
				errs <- "乾 collapsed"
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
}

func TestRecallT2SNormalizeIdempotentStrict(t *testing.T) {
	in := "國漫動畫 臺灣"
	once := Normalize(in).Strict
	twice := Normalize(once).Strict
	if once != twice {
		t.Fatalf("Strict not idempotent: %q → %q", once, twice)
	}
}
