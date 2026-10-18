package matching

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// ---- C.3 / C.2 schema ----

type qualityCasesFile struct {
	Version int           `json:"version"`
	Cases   []qualityCase `json:"cases"`
}

type qualityCase struct {
	ID       string            `json:"id"`
	Bucket   string            `json:"bucket"`
	Files    []qualityFile     `json:"files"`
	Expected []qualityExpected `json:"expected"`
}

type qualityFile struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

type qualityExpected struct {
	Name         string `json:"name"`
	SubjectID    int    `json:"subject_id"`
	StoredNumber int    `json:"stored_number"`
	Mode         string `json:"mode"` // accepted | review | no_candidates
}

type providerReplayFile struct {
	Version      int                   `json:"version"`
	Provenance   string                `json:"provenance"`
	Subjects     []providerSubject     `json:"subjects"`
	Searches     []providerSearch      `json:"searches"`
	Chapters     []providerChapterRule `json:"chapters"`
	UnknownQuery string                `json:"unknown_query"`
}

type providerSubject struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	NameCn       string   `json:"name_cn"`
	Aliases      []string `json:"aliases"`
	Year         int      `json:"year"`
	Media        string   `json:"media"`
	EpisodeCount int      `json:"episode_count"`
	Summary      string   `json:"summary"`
	Cover        string   `json:"cover"`
	Platform     string   `json:"platform"`
}

type providerSearch struct {
	Terms     []string `json:"terms"`
	IDs       []int    `json:"ids"`
	Saturated bool     `json:"saturated"`
}

type providerChapterRule struct {
	SubjectID int `json:"subject_id"`
	Rule      struct {
		FirstEp   int `json:"first_ep"`
		FirstSort int `json:"first_sort"`
		Count     int `json:"count"`
	} `json:"rule"`
}

type holdoutFile struct {
	Version  int           `json:"version"`
	Frozen   bool          `json:"frozen"`
	Note     string        `json:"note"`
	Variants []qualityCase `json:"variants"`
}

type fixtureIndex struct {
	subjects map[int]Subject
	search   map[string]providerSearch // searchKey → entry
	chapters map[int][]Chapter
	unknown  string
}

type fileEval struct {
	mode     DecisionCode
	winnerID int
	stored   int
	reason   string
	chapter  int
}

// TestMatchingQuality 加载附录 C 夹具，跑内核硬门与分桶统计（§8.3）。
func TestMatchingQuality(t *testing.T) {
	cases := loadQualityCases(t)
	if len(cases) == 0 {
		t.Fatal("quality_cases.json 为空")
	}
	fx := loadProviderFixture(t)
	cat, err := LoadCatalog(BuiltinTitleAliasesJSON, nil)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	validateCaseSchema(t, cases)

	buckets := map[string]bucketStat{}
	var (
		autoOK, wrongAccept, reviewN, noCandN int
		failures                              []string
	)

	for _, tc := range cases {
		tc := tc
		t.Run(tc.ID, func(t *testing.T) {
			got := evaluateQualityCase(t, cat, fx, tc)
			for _, exp := range tc.Expected {
				g, ok := got[exp.Name]
				if !ok {
					msg := fmt.Sprintf("%s: missing result for file %q", tc.ID, exp.Name)
					t.Error(msg)
					failures = append(failures, msg)
					continue
				}
				bs := buckets[tc.Bucket]
				switch exp.Mode {
				case "accepted":
					if g.mode != Accepted || g.winnerID != exp.SubjectID {
						msg := fmt.Sprintf("%s/%s: want accepted subject=%d stored=%d; got mode=%s winner=%d stored=%d reason=%s",
							tc.ID, exp.Name, exp.SubjectID, exp.StoredNumber, g.mode, g.winnerID, g.stored, g.reason)
						t.Error(msg)
						failures = append(failures, msg)
						if g.mode == Accepted && g.winnerID != exp.SubjectID {
							wrongAccept++
							bs.WrongAccept++
						}
						break
					}
					if exp.StoredNumber > 0 && g.stored != exp.StoredNumber {
						msg := fmt.Sprintf("%s/%s: stored_number got=%d want=%d", tc.ID, exp.Name, g.stored, exp.StoredNumber)
						t.Error(msg)
						failures = append(failures, msg)
						break
					}
					autoOK++
					bs.AutoOK++
				case "review":
					if g.mode == Accepted {
						msg := fmt.Sprintf("%s/%s: want review, but ACCEPTED subject=%d", tc.ID, exp.Name, g.winnerID)
						t.Error(msg)
						failures = append(failures, msg)
						wrongAccept++
						bs.WrongAccept++
						break
					}
					if g.mode != Review && g.mode != NoCandidates {
						msg := fmt.Sprintf("%s/%s: want review, got %s", tc.ID, exp.Name, g.mode)
						t.Error(msg)
						failures = append(failures, msg)
					}
					reviewN++
					bs.Review++
				case "no_candidates":
					if g.mode == Accepted {
						msg := fmt.Sprintf("%s/%s: want no_candidates, got ACCEPTED %d", tc.ID, exp.Name, g.winnerID)
						t.Error(msg)
						failures = append(failures, msg)
						wrongAccept++
						bs.WrongAccept++
						break
					}
					if g.mode != NoCandidates {
						msg := fmt.Sprintf("%s/%s: want no_candidates, got %s winner=%d reason=%s", tc.ID, exp.Name, g.mode, g.winnerID, g.reason)
						t.Error(msg)
						failures = append(failures, msg)
					}
					noCandN++
					bs.NoCandidates++
				default:
					t.Fatalf("invalid expected mode %q", exp.Mode)
				}
				buckets[tc.Bucket] = bs
			}
		})
	}

	// 4.5 算法层补充（未完整列入 C.3 JSON 的解析用例）
	t.Run("plan45_parse_extras", func(t *testing.T) {
		runPlan45ParseExtras(t)
	})

	// 分桶输出 §8.3
	t.Logf("quality summary: auto_ok=%d wrong_accept=%d review=%d no_candidates=%d cases=%d",
		autoOK, wrongAccept, reviewN, noCandN, len(cases))
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b := buckets[k]
		t.Logf("  bucket %s: auto_ok=%d wrong_accept=%d review=%d no_candidates=%d",
			k, b.AutoOK, b.WrongAccept, b.Review, b.NoCandidates)
	}

	if wrongAccept > 0 {
		t.Fatalf("quality hard gate: %d wrong accepts", wrongAccept)
	}
	if len(failures) > 0 {
		t.Fatalf("quality hard gate FAIL (%d):\n%s", len(failures), strings.Join(failures, "\n"))
	}

	// Holdout 冻结集：终验只读加载，不据其调分；本测试断言文件存在且 frozen。
	assertHoldoutFrozen(t)
}

type bucketStat struct {
	AutoOK, WrongAccept, Review, NoCandidates int
}

func evaluateQualityCase(t *testing.T, cat Catalog, fx *fixtureIndex, tc qualityCase) map[string]fileEval {
	t.Helper()
	inputs := make([]FileInput, 0, len(tc.Files))
	for _, f := range tc.Files {
		inputs = append(inputs, FileInput{Name: f.Name, Dir: f.Dir})
	}
	groups := GroupFiles(inputs)
	out := map[string]fileEval{}
	var batches []BindingBatch

	for _, g := range groups {
		q := g.Query
		if q.Media == "" {
			q.Media = KindUnknown
		}

		unsupported := map[string]bool{}
		for _, f := range g.Files {
			switch f.Episode.Kind {
			case NumberRange, NumberFraction, NumberSpecial:
				unsupported[f.Name] = true
			}
		}
		// 不支持编号：整组 Review，不截断
		if len(unsupported) > 0 {
			for _, f := range g.Files {
				out[f.Name] = fileEval{mode: Review, reason: "不支持编号，强制 Review"}
			}
			continue
		}
		if len(q.Conflicts) > 0 {
			for _, f := range g.Files {
				out[f.Name] = fileEval{mode: Review, reason: "查询冲突：" + strings.Join(q.Conflicts, "；")}
			}
			continue
		}

		subjects, retrievalComplete, ferr := recallSubjects(t, fx, q, cat)
		if ferr != nil {
			t.Fatalf("%s: fixture/search error: %v", tc.ID, ferr)
		}
		if len(subjects) == 0 {
			d := Decide(q, nil, retrievalComplete)
			for _, f := range g.Files {
				out[f.Name] = fileEval{mode: d.Code, reason: d.Reason}
			}
			continue
		}

		chMap := map[int]ChapterDecision{}
		for _, sub := range subjects {
			chs, ok := fx.chapters[sub.ID]
			if !ok {
				t.Fatalf("%s: subject %d missing chapters rule (fixture_error)", tc.ID, sub.ID)
			}
			chMap[sub.ID] = InterpretChapters(g.Files, sub, chs, true)
		}
		ranked := Rank(q, subjects, cat, chMap)
		d := Decide(q, ranked, retrievalComplete)

		batch := BindingBatch{Dir: g.Dir, SubjectID: d.WinnerID, Existing: false}
		for _, f := range g.Files {
			ev := fileEval{mode: d.Code, winnerID: d.WinnerID, reason: d.Reason}
			if d.Code == Accepted && d.WinnerID > 0 {
				if cd, ok := chMap[d.WinnerID]; ok {
					if cd.State == ChapterConflict {
						ev.mode = Review
						ev.winnerID = 0
						ev.reason = "章节解释冲突"
					} else if reason, rejected := cd.Rejected[f.Name]; rejected {
						ev.mode = Review
						ev.winnerID = 0
						ev.reason = reason
					} else {
						for _, b := range cd.Bindings {
							if b.FileName == f.Name {
								ev.stored = b.StoredNumber
								ev.chapter = b.ChapterID
								batch.Bindings = append(batch.Bindings, b)
								break
							}
						}
					}
				}
				if ev.stored == 0 && ev.mode == Accepted {
					for _, fe := range g.Files {
						if fe.Name == f.Name && fe.Episode.Kind == NumberSingle {
							ev.stored = fe.Episode.First
						}
					}
				}
			}
			out[f.Name] = ev
		}
		if d.Code == Accepted && d.WinnerID > 0 && len(batch.Bindings) > 0 {
			batches = append(batches, batch)
		}
	}

	// 跨组 ChapterID / 编号冲突 → 相关新文件 Review
	for _, c := range CheckBindingConflicts(batches) {
		for _, name := range c.Files {
			ev := out[name]
			ev.mode = Review
			ev.winnerID = 0
			ev.reason = c.Reason
			out[name] = ev
		}
	}

	for _, exp := range tc.Expected {
		if _, ok := out[exp.Name]; !ok {
			out[exp.Name] = fileEval{mode: Review, reason: "未落入任何组"}
		}
	}
	return out
}

func recallSubjects(t *testing.T, fx *fixtureIndex, q Query, cat Catalog) ([]Subject, bool, error) {
	t.Helper()
	terms := BuildQueries(q, cat)
	seen := map[int]bool{}
	var out []Subject
	retrievalComplete := true
	anyHit := false
	for _, term := range terms {
		key := searchKey(term.Text)
		entry, ok := fx.lookupSearch(key)
		if !ok {
			// 缩短探测词未登记：当作该路空召回（真实 Bangumi 亦然），不触发 fixture_error。
			// 非缩短未知词仍为夹具/契约错误。
			if term.Shortened {
				continue
			}
			return nil, false, fmt.Errorf("unknown_query fixture_error for term %q (key=%q)", term.Text, key)
		}
		anyHit = true
		for _, id := range entry.IDs {
			if seen[id] {
				continue
			}
			sub, ok := fx.subjects[id]
			if !ok {
				return nil, false, fmt.Errorf("search id %d missing from subjects", id)
			}
			sub.Detailed = false
			seen[id] = true
			out = append(out, sub)
		}
	}
	if !anyHit && len(terms) == 0 {
		retrievalComplete = true
	}
	// 提升为 Detailed=true 副本（内核质量测无需真实 HTTP）
	for i := range out {
		full := fx.subjects[out[i].ID]
		full.Detailed = true
		out[i] = full
	}
	return out, retrievalComplete, nil
}

// lookupSearch：精确键；若为「已登记 base + 明确季号」等价写法，回落到 base（候选集不变）。
func (fx *fixtureIndex) lookupSearch(key string) (providerSearch, bool) {
	if e, ok := fx.search[key]; ok {
		return e, true
	}
	// 拉丁 "Title N" / 中文 "Title 第N季|第X季"
	if base, ok := stripSeasonQualifierKey(key); ok {
		if e, ok := fx.search[base]; ok {
			return e, true
		}
	}
	return providerSearch{}, false
}

func stripSeasonQualifierKey(key string) (string, bool) {
	key = strings.TrimSpace(key)
	// 第N季 / 第X季
	for _, suf := range []string{" 第1季", " 第2季", " 第3季", " 第4季", " 第5季", " 第6季", " 第7季", " 第8季", " 第9季", " 第10季",
		" 第一季", " 第二季", " 第三季", " 第四季", " 第五季", " 第六季", " 第七季", " 第八季", " 第九季", " 第十季"} {
		if strings.HasSuffix(key, suf) {
			return strings.TrimSpace(strings.TrimSuffix(key, suf)), true
		}
	}
	// 拉丁尾数字季（1-2 位）
	fields := strings.Fields(key)
	if len(fields) >= 2 {
		last := fields[len(fields)-1]
		if len(last) <= 2 {
			allDigit := true
			for _, r := range last {
				if r < '0' || r > '9' {
					allDigit = false
					break
				}
			}
			if allDigit {
				n := 0
				fmt.Sscanf(last, "%d", &n)
				if n >= 1 && n <= 24 {
					return strings.Join(fields[:len(fields)-1], " "), true
				}
			}
		}
	}
	return "", false
}

func sameIntSlice(a, b []int) bool {
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

// searchKey：C.1 仅允许 TrimSpace、NFKC、连续空白合并。
func searchKey(s string) string {
	s = strings.TrimSpace(s)
	s = norm.NFKC.String(s)
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func loadQualityCases(t *testing.T) []qualityCase {
	t.Helper()
	path := filepath.Join("testdata", "quality_cases.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc qualityCasesFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if doc.Version != 1 {
		t.Fatalf("quality_cases version=%d want 1", doc.Version)
	}
	return doc.Cases
}

func loadProviderFixture(t *testing.T) *fixtureIndex {
	t.Helper()
	path := filepath.Join("..", "testdata", "matching", "provider_replay.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc providerReplayFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if doc.Version != 1 {
		t.Fatalf("provider_replay version=%d want 1", doc.Version)
	}
	if doc.UnknownQuery != "fixture_error" {
		t.Fatalf("unknown_query=%q want fixture_error", doc.UnknownQuery)
	}
	fx := &fixtureIndex{
		subjects: map[int]Subject{},
		search:   map[string]providerSearch{},
		chapters: map[int][]Chapter{},
		unknown:  doc.UnknownQuery,
	}
	for _, s := range doc.Subjects {
		media := MediaKind(s.Media)
		if media == "" {
			media = KindUnknown
		}
		fx.subjects[s.ID] = Subject{
			ID: s.ID, Name: s.Name, NameCn: s.NameCn,
			Aliases: append([]string(nil), s.Aliases...),
			Year:    s.Year, Media: media, EpisodeCount: s.EpisodeCount,
			Summary: s.Summary, Cover: s.Cover, Platform: s.Platform,
			Detailed: true,
		}
	}
	for _, se := range doc.Searches {
		for _, term := range se.Terms {
			k := searchKey(term)
			if k == "" {
				t.Fatalf("empty search term in fixture")
			}
			// C.1：NFKC 等价词可落到同一键；冲突的 ids 才是夹具错误
			if prev, dup := fx.search[k]; dup {
				if !sameIntSlice(prev.IDs, se.IDs) || prev.Saturated != se.Saturated {
					t.Fatalf("conflicting search key %q", k)
				}
				continue
			}
			fx.search[k] = se
		}
	}
	for _, ch := range doc.Chapters {
		r := ch.Rule
		if r.Count <= 0 {
			t.Fatalf("chapters subject %d count<=0", ch.SubjectID)
		}
		list := make([]Chapter, 0, r.Count)
		for i := 0; i < r.Count; i++ {
			list = append(list, Chapter{
				ID:   ch.SubjectID*10000 + i + 1,
				Ep:   float64(r.FirstEp + i),
				Sort: float64(r.FirstSort + i),
			})
		}
		if _, ok := fx.chapters[ch.SubjectID]; ok {
			t.Fatalf("duplicate chapters for %d", ch.SubjectID)
		}
		fx.chapters[ch.SubjectID] = list
	}
	return fx
}

func validateCaseSchema(t *testing.T, cases []qualityCase) {
	t.Helper()
	seen := map[string]bool{}
	for _, tc := range cases {
		if strings.TrimSpace(tc.ID) == "" {
			t.Fatal("case missing id")
		}
		if seen[tc.ID] {
			t.Fatalf("duplicate id %q", tc.ID)
		}
		seen[tc.ID] = true
		if strings.TrimSpace(tc.Bucket) == "" {
			t.Fatalf("%s: missing bucket", tc.ID)
		}
		if len(tc.Files) == 0 || len(tc.Expected) == 0 {
			t.Fatalf("%s: files/expected empty", tc.ID)
		}
		names := map[string]bool{}
		for _, f := range tc.Files {
			if f.Name == "" {
				t.Fatalf("%s: empty file name", tc.ID)
			}
			names[f.Name] = true
		}
		for _, e := range tc.Expected {
			if !names[e.Name] {
				t.Fatalf("%s: expected name %q not in files", tc.ID, e.Name)
			}
			switch e.Mode {
			case "accepted", "review", "no_candidates":
			default:
				t.Fatalf("%s: invalid mode %q", tc.ID, e.Mode)
			}
			if e.Mode == "accepted" && e.SubjectID <= 0 {
				t.Fatalf("%s: accepted requires subject_id>0", tc.ID)
			}
		}
	}
}

func assertHoldoutFrozen(t *testing.T) {
	t.Helper()
	path := filepath.Join("testdata", "quality_holdout_variants.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("holdout missing: %v", err)
	}
	var doc holdoutFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("holdout parse: %v", err)
	}
	if !doc.Frozen || doc.Version != 1 || len(doc.Variants) == 0 {
		t.Fatalf("holdout not frozen properly: frozen=%v version=%d n=%d", doc.Frozen, doc.Version, len(doc.Variants))
	}
	t.Logf("holdout frozen variants=%d (not scored in this run; final verification only)", len(doc.Variants))
}

func runPlan45ParseExtras(t *testing.T) {
	t.Helper()
	// 【推しの子】第2期
	ev := ParseFile(FileInput{Name: "【推しの子】 第2期 - 01.mkv"})
	if ev.Ordinal.Season != 2 || ev.Episode.First != 1 {
		t.Fatalf("推しの子: season=%d ep=%d", ev.Ordinal.Season, ev.Episode.First)
	}
	found := false
	for _, ti := range ev.Titles {
		if strings.Contains(ti.Text, "推しの子") {
			found = true
		}
	}
	if !found {
		t.Fatalf("推しの子 title lost: %#v", ev.Titles)
	}
	// U-17
	ev2 := ParseFile(FileInput{Name: "U-17 Show - 03.mkv"})
	if ev2.Episode.First != 3 {
		t.Fatalf("U-17 ep=%d", ev2.Episode.First)
	}
	okU := false
	for _, ti := range ev2.Titles {
		if strings.Contains(strings.ToUpper(ti.Text), "U-17") {
			okU = true
		}
	}
	if !okU {
		t.Fatalf("U-17 title lost: %#v", ev2.Titles)
	}
	// 四位集号 / 第2023话 不作年份
	ev3 := ParseFile(FileInput{Name: "One.Piece.S01E1100.1080p.mkv"})
	if ev3.Episode.First != 1100 || ev3.Ordinal.Season != 1 {
		t.Fatalf("S01E1100 got season=%d ep=%d", ev3.Ordinal.Season, ev3.Episode.First)
	}
	ev4 := ParseFile(FileInput{Name: "海贼王 第2023话.mkv"})
	if ev4.Episode.First != 2023 {
		t.Fatalf("第2023话 ep=%d", ev4.Episode.First)
	}
	if ev4.Year == 2023 {
		t.Fatalf("第2023话 must not set Year")
	}
}
