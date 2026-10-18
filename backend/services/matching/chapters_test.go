package matching

import (
	"testing"
)

func TestChaptersSortGapRejected(t *testing.T) {
	// sort 为 25、27，文件 26 → 不因包围接受
	files := []FileEvidence{
		{Name: "Show - 26.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 26, Last: 26, Explicit: true}},
	}
	subject := Subject{ID: 1, Name: "Show", EpisodeCount: 48, Media: KindSeries, Detailed: true}
	chapters := []Chapter{
		{ID: 100, Ep: 1, Sort: 25},
		{ID: 101, Ep: 2, Sort: 27},
	}
	d := InterpretChapters(files, subject, chapters, true)
	if d.State != ChapterConflict {
		t.Fatalf("sort gap must Conflict, got %s rejected=%v bindings=%v", d.State, d.Rejected, d.Bindings)
	}
	if _, ok := d.Rejected["Show - 26.mkv"]; !ok {
		t.Fatalf("file 26 must be rejected: %v", d.Rejected)
	}
}

func TestChaptersSortExactAccepted(t *testing.T) {
	files := []FileEvidence{
		{Name: "a.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 25, Last: 25, Explicit: true}},
		{Name: "b.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 26, Last: 26, Explicit: true}},
	}
	subject := Subject{ID: 1, EpisodeCount: 24, Media: KindSeries}
	chapters := []Chapter{
		{ID: 201, Ep: 1, Sort: 25},
		{ID: 202, Ep: 2, Sort: 26},
	}
	d := InterpretChapters(files, subject, chapters, true)
	if d.State != ChapterConfirmed {
		t.Fatalf("exact sort map must Confirmed: %s %v %v", d.State, d.Rejected, d.Bindings)
	}
	if len(d.Bindings) != 2 {
		t.Fatalf("bindings=%v", d.Bindings)
	}
	for _, b := range d.Bindings {
		if b.StoredNumber != 25 && b.StoredNumber != 26 {
			t.Fatalf("StoredNumber must stay original: %+v", b)
		}
		if b.Basis != "sort" {
			t.Fatalf("basis=%s want sort", b.Basis)
		}
		if b.ChapterID == 0 {
			t.Fatalf("ChapterID required: %+v", b)
		}
	}
}

func TestChaptersEpSortConflict(t *testing.T) {
	files := []FileEvidence{
		{Name: "x.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 5, Last: 5}},
	}
	subject := Subject{ID: 1, Media: KindSeries}
	chapters := []Chapter{
		{ID: 1, Ep: 5, Sort: 10},
		{ID: 2, Ep: 9, Sort: 5},
	}
	d := InterpretChapters(files, subject, chapters, true)
	if d.State != ChapterConflict {
		t.Fatalf("Ep/Sort different chapters must Conflict: %s %v", d.State, d.Rejected)
	}
}

func TestChaptersDuplicateChapterID(t *testing.T) {
	files := []FileEvidence{
		{Name: "01.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 1, Last: 1}},
		{Name: "25.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 25, Last: 25}},
	}
	subject := Subject{ID: 1, Media: KindSeries}
	// 两个号都映射到同一 ChapterID（Ep=1 与 Sort=25 指向同一章不现实；构造 Ep1=章A，Sort25=章A）
	chapters := []Chapter{
		{ID: 50, Ep: 1, Sort: 25},
	}
	d := InterpretChapters(files, subject, chapters, true)
	if d.State != ChapterConflict {
		t.Fatalf("duplicate ChapterID must Conflict: %s rejected=%v bindings=%v", d.State, d.Rejected, d.Bindings)
	}
	if len(d.Rejected) < 2 {
		t.Fatalf("both files rejected: %v", d.Rejected)
	}
}

func TestChaptersUnsupportedNumbering(t *testing.T) {
	cases := []FileEvidence{
		{Name: "r.mkv", Episode: EpisodeToken{Kind: NumberRange, First: 1, Last: 2, Raw: "01-02"}},
		{Name: "f.mkv", Episode: EpisodeToken{Kind: NumberFraction, Raw: "12.5"}},
		{Name: "s.mkv", Episode: EpisodeToken{Kind: NumberSpecial, Raw: "第0话"}},
	}
	subject := Subject{ID: 1, EpisodeCount: 24, Media: KindSeries}
	for _, f := range cases {
		d := InterpretChapters([]FileEvidence{f}, subject, nil, true)
		if d.State != ChapterConflict {
			t.Fatalf("%s: want Conflict got %s", f.Name, d.State)
		}
		if _, ok := d.Rejected[f.Name]; !ok {
			t.Fatalf("%s: not rejected %v", f.Name, d.Rejected)
		}
	}
}

func TestChaptersCountFit(t *testing.T) {
	files := []FileEvidence{
		{Name: "05.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 5, Last: 5}},
	}
	subject := Subject{ID: 1, EpisodeCount: 12, Media: KindSeries}
	d := InterpretChapters(files, subject, nil, true) // 空列表但 known
	if d.State != ChapterCountFit {
		t.Fatalf("count fit: %s %v %v", d.State, d.Rejected, d.Bindings)
	}
	if len(d.Bindings) != 1 || d.Bindings[0].Basis != "count" || d.Bindings[0].StoredNumber != 5 {
		t.Fatalf("%+v", d.Bindings)
	}
}

func TestChaptersCountOverflow(t *testing.T) {
	files := []FileEvidence{
		{Name: "25.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 25, Last: 25}},
	}
	subject := Subject{ID: 1, EpisodeCount: 12, Media: KindSeries}
	d := InterpretChapters(files, subject, nil, true)
	if d.State != ChapterConflict {
		t.Fatalf("overflow must Conflict: %s %v", d.State, d.Rejected)
	}
}

func TestChaptersUncheckedWhenUnknown(t *testing.T) {
	files := []FileEvidence{
		{Name: "05.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 5, Last: 5}},
	}
	d := InterpretChapters(files, Subject{ID: 1}, nil, false)
	if d.State != ChapterUnchecked {
		t.Fatalf("got %s", d.State)
	}
}

func TestChaptersBindingConflictsCrossGroup(t *testing.T) {
	// 两组新文件映射同 ChapterID
	batches := []BindingBatch{
		{
			Dir: "anime/show", SubjectID: 1, Existing: false,
			Bindings: []EpisodeBinding{{FileName: "01.mkv", StoredNumber: 1, ChapterID: 9, Basis: "ep"}},
		},
		{
			Dir: "anime/show", SubjectID: 1, Existing: false,
			Bindings: []EpisodeBinding{{FileName: "25.mkv", StoredNumber: 25, ChapterID: 9, Basis: "sort"}},
		},
	}
	conflicts := CheckBindingConflicts(batches)
	if len(conflicts) == 0 {
		t.Fatal("expected cross-group ChapterID conflict")
	}
	files := map[string]bool{}
	for _, c := range conflicts {
		for _, f := range c.Files {
			files[f] = true
		}
	}
	if !files["01.mkv"] || !files["25.mkv"] {
		t.Fatalf("both new files in conflict: %+v", conflicts)
	}
	// 换组顺序结果相同
	batchesRev := []BindingBatch{batches[1], batches[0]}
	conflicts2 := CheckBindingConflicts(batchesRev)
	if len(conflicts2) == 0 {
		t.Fatal("order must not matter")
	}
}

func TestChaptersBindingConflictsKeepExisting(t *testing.T) {
	batches := []BindingBatch{
		{
			Dir: "d", SubjectID: 1, Existing: true,
			Bindings: []EpisodeBinding{{FileName: "01.mkv", StoredNumber: 1, ChapterID: 9, Basis: "ep"}},
		},
		{
			Dir: "d", SubjectID: 1, Existing: false,
			Bindings: []EpisodeBinding{{FileName: "25.mkv", StoredNumber: 25, ChapterID: 9, Basis: "sort"}},
		},
	}
	conflicts := CheckBindingConflicts(batches)
	if len(conflicts) == 0 {
		t.Fatal("expected conflict")
	}
	for _, c := range conflicts {
		for _, f := range c.Files {
			if f == "01.mkv" {
				t.Fatalf("existing file must not be in reject list: %+v", c)
			}
		}
		found := false
		for _, f := range c.Files {
			if f == "25.mkv" {
				found = true
			}
		}
		if !found {
			t.Fatalf("new file 25 must be rejected: %+v", c)
		}
	}
}

func TestChaptersDuplicateStoredNumberInGroup(t *testing.T) {
	files := []FileEvidence{
		{Name: "a.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 3, Last: 3}},
		{Name: "b.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 3, Last: 3}},
	}
	subject := Subject{ID: 1, EpisodeCount: 12}
	d := InterpretChapters(files, subject, nil, true)
	if d.State != ChapterConflict {
		t.Fatalf("dup StoredNumber: %s %v", d.State, d.Rejected)
	}
}
