package matching

import (
	"strings"
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
