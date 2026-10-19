package matching

import "testing"

func TestTitlesShareStrongIdentityRejectsFuzzy(t *testing.T) {
	cat, err := LoadCatalog(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if TitlesShareStrongIdentity("Alpha Animation", "Beta Animation", cat) {
		t.Fatal("fuzzy cross-title must not count as strong identity")
	}
	if !TitlesShareStrongIdentity("Show", "Show", cat) {
		t.Fatal("exact title must share identity")
	}
	if !TitlesShareStrongIdentity("Show", "show", cat) {
		t.Fatal("normal-case title must share identity")
	}
}

func TestFileEvidenceShareWorkRenameVsCrossWork(t *testing.T) {
	cat, err := LoadCatalog(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sameOld := ParseFile(FileInput{Name: "Show - 01 [OLD].mkv", Dir: "d"})
	sameNew := ParseFile(FileInput{Name: "Show - 01 [NEW].mkv", Dir: "d"})
	if !FileEvidenceShareWork(sameOld, sameNew, cat) {
		t.Fatalf("same-work rename must share identity: old=%v new=%v", sameOld.Titles, sameNew.Titles)
	}
	alpha := ParseFile(FileInput{Name: "Alpha Animation - 01.mkv", Dir: "d"})
	beta := ParseFile(FileInput{Name: "Beta Animation - 01.mkv", Dir: "d"})
	if FileEvidenceShareWork(alpha, beta, cat) {
		t.Fatalf("cross-work must not share identity: alpha=%v beta=%v", alpha.Titles, beta.Titles)
	}
}
