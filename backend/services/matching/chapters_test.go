package matching

import (
	"fmt"
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

func TestChaptersIsolateFractionKeepsNormalBindings(t *testing.T) {
	files := make([]FileEvidence, 0, 13)
	for i := 1; i <= 12; i++ {
		files = append(files, FileEvidence{
			Name:    "Review Show - " + fmt.Sprintf("%02d", i) + ".mkv",
			Episode: EpisodeToken{Kind: NumberSingle, First: i, Last: i, Explicit: true},
		})
	}
	files = append(files, FileEvidence{
		Name:    "Review Show - 06.5.mkv",
		Episode: EpisodeToken{Kind: NumberFraction, Raw: "06.5", Explicit: true},
	})
	subject := Subject{ID: 1, Name: "Review Show", EpisodeCount: 12, Media: KindSeries, Detailed: true}
	chapters := make([]Chapter, 0, 12)
	for i := 1; i <= 12; i++ {
		chapters = append(chapters, Chapter{ID: 100 + i, Ep: float64(i), Sort: float64(i)})
	}
	d := InterpretChapters(files, subject, chapters, true)
	if d.State != ChapterConfirmed {
		t.Fatalf("normal files must stay Confirmed, got %s rejected=%v bindings=%d", d.State, d.Rejected, len(d.Bindings))
	}
	if len(d.Bindings) != 12 {
		t.Fatalf("want 12 bindings, got %d", len(d.Bindings))
	}
	if _, ok := d.Rejected["Review Show - 06.5.mkv"]; !ok {
		t.Fatalf("fraction must be rejected: %v", d.Rejected)
	}
}

func TestChaptersIsolateOutOfRangeKeepsNormalBindings(t *testing.T) {
	files := make([]FileEvidence, 0, 13)
	for i := 1; i <= 12; i++ {
		files = append(files, FileEvidence{
			Name:    "Review Show - " + fmt.Sprintf("%02d", i) + ".mkv",
			Episode: EpisodeToken{Kind: NumberSingle, First: i, Last: i, Explicit: true},
		})
	}
	files = append(files, FileEvidence{
		Name:    "Review Show - 13.mkv",
		Episode: EpisodeToken{Kind: NumberSingle, First: 13, Last: 13, Explicit: true},
	})
	subject := Subject{ID: 1, Name: "Review Show", EpisodeCount: 12, Media: KindSeries, Detailed: true}
	chapters := make([]Chapter, 0, 12)
	for i := 1; i <= 12; i++ {
		chapters = append(chapters, Chapter{ID: 200 + i, Ep: float64(i), Sort: float64(i)})
	}
	d := InterpretChapters(files, subject, chapters, true)
	if d.State != ChapterConfirmed {
		t.Fatalf("got %s rejected=%v", d.State, d.Rejected)
	}
	if len(d.Bindings) != 12 {
		t.Fatalf("bindings=%d", len(d.Bindings))
	}
	if _, ok := d.Rejected["Review Show - 13.mkv"]; !ok {
		t.Fatalf("ep13 must be rejected: %v", d.Rejected)
	}
}

func TestChaptersIsolateDuplicateVersionKeepsOthers(t *testing.T) {
	files := make([]FileEvidence, 0, 13)
	for i := 1; i <= 12; i++ {
		if i == 5 {
			continue
		}
		files = append(files, FileEvidence{
			Name:    "Review Show - " + fmt.Sprintf("%02d", i) + ".mkv",
			Episode: EpisodeToken{Kind: NumberSingle, First: i, Last: i, Explicit: true},
		})
	}
	files = append(files,
		FileEvidence{Name: "Review Show - 05.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 5, Last: 5, Explicit: true}},
		FileEvidence{Name: "Review Show - 05v2.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 5, Last: 5, Raw: "05", Explicit: true}},
	)
	subject := Subject{ID: 1, Name: "Review Show", EpisodeCount: 12, Media: KindSeries, Detailed: true}
	chapters := make([]Chapter, 0, 12)
	for i := 1; i <= 12; i++ {
		chapters = append(chapters, Chapter{ID: 300 + i, Ep: float64(i), Sort: float64(i)})
	}
	d := InterpretChapters(files, subject, chapters, true)
	if d.State != ChapterConfirmed {
		t.Fatalf("got %s rejected=%v bindings=%d", d.State, d.Rejected, len(d.Bindings))
	}
	if len(d.Bindings) != 11 {
		t.Fatalf("want 11 bindings (exclude both 05 versions), got %d %#v", len(d.Bindings), d.Bindings)
	}
	if _, ok := d.Rejected["Review Show - 05.mkv"]; !ok {
		t.Fatalf("05 must be rejected: %v", d.Rejected)
	}
	if _, ok := d.Rejected["Review Show - 05v2.mkv"]; !ok {
		t.Fatalf("05v2 must be rejected: %v", d.Rejected)
	}
}

func assertCoverage(t *testing.T, got ChapterCoverage, required, matched, missing []int) {
	t.Helper()
	if required == nil {
		required = []int{}
	}
	if matched == nil {
		matched = []int{}
	}
	if missing == nil {
		missing = []int{}
	}
	if got.Required == nil || got.Matched == nil || got.Missing == nil {
		t.Fatalf("Coverage slices must be non-nil: %+v", got)
	}
	if !intSlicesEqual(got.Required, required) {
		t.Fatalf("Required=%v want %v", got.Required, required)
	}
	if !intSlicesEqual(got.Matched, matched) {
		t.Fatalf("Matched=%v want %v", got.Matched, matched)
	}
	if !intSlicesEqual(got.Missing, missing) {
		t.Fatalf("Missing=%v want %v", got.Missing, missing)
	}
}

func intSlicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestRecallCoverageComplete：4.7 完整覆盖 51/52。
func TestRecallCoverageComplete(t *testing.T) {
	files := []FileEvidence{
		{Name: "Show - 51.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 51, Last: 51, Explicit: true}},
		{Name: "Show - 52.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 52, Last: 52, Explicit: true}},
	}
	subject := Subject{ID: 1, Name: "Show", EpisodeCount: 8, Media: KindSeries, Detailed: true}
	chapters := []Chapter{
		{ID: 501, Ep: 51, Sort: 1},
		{ID: 502, Ep: 52, Sort: 2},
	}
	d := InterpretChapters(files, subject, chapters, true)
	if d.State != ChapterConfirmed {
		t.Fatalf("State=%s want confirmed rejected=%v", d.State, d.Rejected)
	}
	if len(d.Bindings) != 2 {
		t.Fatalf("bindings=%v", d.Bindings)
	}
	for _, b := range d.Bindings {
		if b.StoredNumber != 51 && b.StoredNumber != 52 {
			t.Fatalf("StoredNumber must stay original: %+v", b)
		}
		if b.ChapterID == 0 || (b.Basis != "ep" && b.Basis != "sort") {
			t.Fatalf("want real ep/sort binding: %+v", b)
		}
	}
	assertCoverage(t, d.Coverage, []int{51, 52}, []int{51, 52}, []int{})
}

// TestRecallCoveragePartial：仅命中 51 时 Confirmed 但 Missing=[52]。
func TestRecallCoveragePartial(t *testing.T) {
	files := []FileEvidence{
		{Name: "Show - 51.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 51, Last: 51, Explicit: true}},
		{Name: "Show - 52.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 52, Last: 52, Explicit: true}},
	}
	subject := Subject{ID: 1, Name: "Show", EpisodeCount: 8, Media: KindSeries, Detailed: true}
	chapters := []Chapter{
		{ID: 501, Ep: 51, Sort: 1},
	}
	d := InterpretChapters(files, subject, chapters, true)
	if d.State != ChapterConfirmed {
		t.Fatalf("partial real hit still Confirmed, got %s rejected=%v bindings=%v", d.State, d.Rejected, d.Bindings)
	}
	assertCoverage(t, d.Coverage, []int{51, 52}, []int{51}, []int{52})
	if _, ok := d.Rejected["Show - 52.mkv"]; !ok {
		t.Fatalf("52 must be rejected when no exact chapter: %v", d.Rejected)
	}
}

// TestRecallCoverageDuplicateVersion：05/05v2 使 Required 含 5 但不计入 Matched。
func TestRecallCoverageDuplicateVersion(t *testing.T) {
	files := make([]FileEvidence, 0, 13)
	for i := 1; i <= 12; i++ {
		if i == 5 {
			continue
		}
		files = append(files, FileEvidence{
			Name:    "Review Show - " + fmt.Sprintf("%02d", i) + ".mkv",
			Episode: EpisodeToken{Kind: NumberSingle, First: i, Last: i, Explicit: true},
		})
	}
	files = append(files,
		FileEvidence{Name: "Review Show - 05.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 5, Last: 5, Explicit: true}},
		FileEvidence{Name: "Review Show - 05v2.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 5, Last: 5, Raw: "05", Explicit: true}},
	)
	subject := Subject{ID: 1, Name: "Review Show", EpisodeCount: 12, Media: KindSeries, Detailed: true}
	chapters := make([]Chapter, 0, 12)
	for i := 1; i <= 12; i++ {
		chapters = append(chapters, Chapter{ID: 300 + i, Ep: float64(i), Sort: float64(i)})
	}
	d := InterpretChapters(files, subject, chapters, true)
	if d.State != ChapterConfirmed {
		t.Fatalf("got %s", d.State)
	}
	wantRequired := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	wantMatched := []int{1, 2, 3, 4, 6, 7, 8, 9, 10, 11, 12}
	assertCoverage(t, d.Coverage, wantRequired, wantMatched, []int{5})
}

// TestRecallCoverageCountFitDoesNotMatch：CountFit 填 Required，Matched 空。
func TestRecallCoverageCountFitDoesNotMatch(t *testing.T) {
	files := []FileEvidence{
		{Name: "05.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 5, Last: 5}},
	}
	subject := Subject{ID: 1, EpisodeCount: 12, Media: KindSeries}
	d := InterpretChapters(files, subject, nil, true)
	if d.State != ChapterCountFit {
		t.Fatalf("got %s", d.State)
	}
	if len(d.Bindings) != 1 || d.Bindings[0].Basis != "count" {
		t.Fatalf("bindings=%v", d.Bindings)
	}
	assertCoverage(t, d.Coverage, []int{5}, []int{}, []int{5})
}

// TestRecallCoverageUnknownChapters：chaptersKnown=false 仍填 Required，Matched 空。
func TestRecallCoverageUnknownChapters(t *testing.T) {
	files := []FileEvidence{
		{Name: "05.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 5, Last: 5}},
		{Name: "13.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 13, Last: 13}},
	}
	d := InterpretChapters(files, Subject{ID: 1}, nil, false)
	if d.State != ChapterUnchecked {
		t.Fatalf("got %s", d.State)
	}
	if len(d.Bindings) != 0 {
		t.Fatalf("bindings must be empty: %v", d.Bindings)
	}
	assertCoverage(t, d.Coverage, []int{5, 13}, []int{}, []int{5, 13})
}

// TestRecallCoverageOutOfRangeAndFraction：越界进 Required；小数/范围/特别篇不进。
func TestRecallCoverageOutOfRangeAndFraction(t *testing.T) {
	files := make([]FileEvidence, 0, 14)
	for i := 1; i <= 12; i++ {
		files = append(files, FileEvidence{
			Name:    "Review Show - " + fmt.Sprintf("%02d", i) + ".mkv",
			Episode: EpisodeToken{Kind: NumberSingle, First: i, Last: i, Explicit: true},
		})
	}
	files = append(files,
		FileEvidence{Name: "Review Show - 13.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 13, Last: 13, Explicit: true}},
		FileEvidence{Name: "Review Show - 06.5.mkv", Episode: EpisodeToken{Kind: NumberFraction, Raw: "06.5", Explicit: true}},
		FileEvidence{Name: "Review Show - 01-02.mkv", Episode: EpisodeToken{Kind: NumberRange, First: 1, Last: 2, Raw: "01-02"}},
		FileEvidence{Name: "Review Show - SP.mkv", Episode: EpisodeToken{Kind: NumberSpecial, Raw: "第0话"}},
	)
	subject := Subject{ID: 1, Name: "Review Show", EpisodeCount: 12, Media: KindSeries, Detailed: true}
	chapters := make([]Chapter, 0, 12)
	for i := 1; i <= 12; i++ {
		chapters = append(chapters, Chapter{ID: 200 + i, Ep: float64(i), Sort: float64(i)})
	}
	d := InterpretChapters(files, subject, chapters, true)
	if d.State != ChapterConfirmed {
		t.Fatalf("got %s rejected=%v", d.State, d.Rejected)
	}
	wantRequired := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}
	wantMatched := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	assertCoverage(t, d.Coverage, wantRequired, wantMatched, []int{13})
}

// TestRecallCoverageEmptySinglesNonNil：无正整数 Single 时切片仍非 nil。
func TestRecallCoverageEmptySinglesNonNil(t *testing.T) {
	files := []FileEvidence{
		{Name: "f.mkv", Episode: EpisodeToken{Kind: NumberFraction, Raw: "12.5"}},
	}
	d := InterpretChapters(files, Subject{ID: 1, EpisodeCount: 12}, nil, true)
	assertCoverage(t, d.Coverage, []int{}, []int{}, []int{})
}

// TestRecallCoverageMoviePlaceholderNotMatched：movie 占位不因 Basis=movie 计入 Matched。
func TestRecallCoverageMoviePlaceholderNotMatched(t *testing.T) {
	files := []FileEvidence{
		{Name: "Movie.mkv", Media: KindMovie, Episode: EpisodeToken{Kind: NumberUnknown}},
	}
	subject := Subject{ID: 1, Media: KindMovie, EpisodeCount: 1}
	d := InterpretChapters(files, subject, nil, true)
	// 无 NumberSingle → Required 空；即便有 movie 绑定也不能伪造 Matched
	assertCoverage(t, d.Coverage, []int{}, []int{}, []int{})
}
