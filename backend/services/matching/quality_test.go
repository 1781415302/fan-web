package matching

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// qualityCase 统一 case schema（§8.3）。
// ATTACHMENT_GAP：当前仅含 synthetic / 手写 4.5 安全与仙王用例，不含 A/B 语料回放。
type qualityCase struct {
	ID                 string                      `json:"id"`
	Split              string                      `json:"split"`
	Note               string                      `json:"note,omitempty"`
	Files              []qualityFile               `json:"files"`
	Expected           []qualityExpected           `json:"expected"`
	CandidateSnapshots []qualitySubjectSnap        `json:"candidate_snapshots"`
	ChapterSnapshots   map[string][]qualityChapter `json:"chapter_snapshots"`
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

type qualitySubjectSnap struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	NameCn       string   `json:"name_cn"`
	Year         int      `json:"year"`
	Media        string   `json:"media"`
	Aliases      []string `json:"aliases"`
	Platform     string   `json:"platform"`
	EpisodeCount int      `json:"episode_count"`
	Detailed     bool     `json:"detailed"`
}

type qualityChapter struct {
	ID   int     `json:"id"`
	Ep   float64 `json:"ep"`
	Sort float64 `json:"sort"`
}

// TestMatchingQuality 加载统一夹具并做内核级对照。
// 三 zip 附件缺失时不得宣称 A/B 质量门通过；本测试只保证夹具 schema、
// 4.5 安全/仙王 synthetic 期望合法，以及对当前内核可验证的用例做回归。
func TestMatchingQuality(t *testing.T) {
	cases := loadQualityCases(t)
	if len(cases) == 0 {
		t.Fatal("quality_cases.json 为空：S6 至少需要 synthetic 用例以保持 TestMatchingQuality 可发现")
	}

	cat, err := LoadCatalog(BuiltinTitleAliasesJSON, nil)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}

	assertNoFrozenWrongAccepts(t, cases)
	validateCaseSchema(t, cases)

	buckets := map[string]int{}
	var (
		autoOK, wrongAccept, reviewN, noCandN int
	)

	for _, tc := range cases {
		tc := tc
		t.Run(tc.ID, func(t *testing.T) {
			got := evaluateQualityCase(t, cat, tc)
			for _, exp := range tc.Expected {
				g, ok := got[exp.Name]
				if !ok {
					t.Fatalf("missing result for file %q", exp.Name)
				}
				buckets[bucketKey(tc, exp)]++

				// 解析层：StoredNumber 期望 >0 时核对 Parse 结果
				if exp.StoredNumber > 0 {
					ev := ParseFile(FileInput{Name: exp.Name, Dir: dirOf(tc, exp.Name)})
					if ev.Episode.Kind == NumberSingle && ev.Episode.First != exp.StoredNumber {
						t.Errorf("parse StoredNumber: file=%s got=%d want=%d", exp.Name, ev.Episode.First, exp.StoredNumber)
					}
				}

				switch exp.Mode {
				case "accepted":
					if g.mode != Accepted {
						t.Errorf("want accepted subject=%d, got mode=%s winner=%d reason=%s",
							exp.SubjectID, g.mode, g.winnerID, g.reason)
						break
					}
					if g.winnerID != exp.SubjectID {
						t.Errorf("WRONG ACCEPT: file=%s got subject=%d want=%d", exp.Name, g.winnerID, exp.SubjectID)
						wrongAccept++
						break
					}
					if exp.StoredNumber > 0 && g.stored != 0 && g.stored != exp.StoredNumber {
						t.Errorf("stored_number: file=%s got=%d want=%d", exp.Name, g.stored, exp.StoredNumber)
					}
					autoOK++
				case "review":
					if g.mode == Accepted {
						t.Errorf("want review, but ACCEPTED subject=%d (must not freeze wrong accept)", g.winnerID)
						wrongAccept++
						break
					}
					if g.mode != Review && g.mode != NoCandidates {
						t.Errorf("want review-ish, got %s", g.mode)
					}
					reviewN++
				case "no_candidates":
					if g.mode != NoCandidates {
						// 允许 Review（召回空时内核返回 no_candidates；若有噪声候选则 review）
						if g.mode == Accepted {
							t.Errorf("want no_candidates, got ACCEPTED %d", g.winnerID)
							wrongAccept++
						} else {
							t.Logf("no_candidates case got %s (acceptable under ATTACHMENT_GAP synthetic)", g.mode)
						}
					}
					noCandN++
				default:
					t.Fatalf("invalid expected mode %q", exp.Mode)
				}
			}
		})
	}

	t.Logf("ATTACHMENT_GAP: A/B 语料三 zip 未提供；本测试仅 synthetic/手写夹具，不宣称质量门 PASS")
	t.Logf("bucket totals: auto_ok=%d wrong_accept=%d review=%d no_candidates=%d cases=%d",
		autoOK, wrongAccept, reviewN, noCandN, len(cases))
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("  bucket %s = %d", k, buckets[k])
	}
	if wrongAccept > 0 {
		t.Fatalf("quality hard gate: %d wrong accepts", wrongAccept)
	}
}

type fileEval struct {
	mode     DecisionCode
	winnerID int
	stored   int
	reason   string
}

func evaluateQualityCase(t *testing.T, cat Catalog, tc qualityCase) map[string]fileEval {
	t.Helper()
	inputs := make([]FileInput, 0, len(tc.Files))
	for _, f := range tc.Files {
		inputs = append(inputs, FileInput{Name: f.Name, Dir: f.Dir})
	}
	groups := GroupFiles(inputs)
	subjects := snapsToSubjects(tc.CandidateSnapshots)
	out := map[string]fileEval{}

	// 先按组决策，再按文件落地 mode / stored
	for _, g := range groups {
		q := normalizeQueryMedia(g.Query)

		// 不支持编号的文件：强制 Review（4.5），不截断
		unsupported := map[string]bool{}
		for _, f := range g.Files {
			switch f.Episode.Kind {
			case NumberRange, NumberFraction, NumberSpecial:
				unsupported[f.Name] = true
			}
		}

		if len(subjects) == 0 {
			d := Decide(q, nil, true)
			for _, f := range g.Files {
				mode := d.Code
				if unsupported[f.Name] {
					mode = Review
				}
				out[f.Name] = fileEval{mode: mode, reason: d.Reason}
			}
			continue
		}

		// 章节：对每个候选先 Interpret，再一并交给 Rank
		chMap := map[int]ChapterDecision{}
		for _, sub := range subjects {
			chs, known := chaptersFor(tc, sub.ID)
			if !known {
				continue
			}
			chMap[sub.ID] = InterpretChapters(g.Files, sub, chs, true)
		}

		ranked := Rank(q, subjects, cat, chMap)
		d := Decide(q, ranked, true)

		for _, f := range g.Files {
			ev := fileEval{mode: d.Code, winnerID: d.WinnerID, reason: d.Reason}
			if unsupported[f.Name] {
				ev.mode = Review
				ev.winnerID = 0
				ev.reason = "不支持编号，强制 Review"
				out[f.Name] = ev
				continue
			}
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
								break
							}
						}
					}
				}
				// 无章节绑定时用解析 Single
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
	}

	// 确保每个期望文件都有结果（防御 group 遗漏）
	for _, exp := range tc.Expected {
		if _, ok := out[exp.Name]; !ok {
			out[exp.Name] = fileEval{mode: Review, reason: "未落入任何组"}
		}
	}
	return out
}

func normalizeQueryMedia(q Query) Query {
	if q.Media == "" || q.Media == MediaKind("") {
		q.Media = KindUnknown
	}
	return q
}

func snapsToSubjects(snaps []qualitySubjectSnap) []Subject {
	out := make([]Subject, 0, len(snaps))
	for _, s := range snaps {
		media := MediaKind(s.Media)
		if media == "" {
			media = KindUnknown
		}
		out = append(out, Subject{
			ID:           s.ID,
			Name:         s.Name,
			NameCn:       s.NameCn,
			Year:         s.Year,
			Media:        media,
			Aliases:      append([]string(nil), s.Aliases...),
			Platform:     s.Platform,
			EpisodeCount: s.EpisodeCount,
			Detailed:     s.Detailed,
		})
	}
	return out
}

func chaptersFor(tc qualityCase, subjectID int) ([]Chapter, bool) {
	if tc.ChapterSnapshots == nil {
		return nil, false
	}
	key := fmt.Sprintf("%d", subjectID)
	raw, ok := tc.ChapterSnapshots[key]
	if !ok {
		return nil, false
	}
	out := make([]Chapter, 0, len(raw))
	for _, c := range raw {
		out = append(out, Chapter{ID: c.ID, Ep: c.Ep, Sort: c.Sort})
	}
	return out, true
}

func dirOf(tc qualityCase, name string) string {
	for _, f := range tc.Files {
		if f.Name == name {
			return f.Dir
		}
	}
	return ""
}

func loadQualityCases(t *testing.T) []qualityCase {
	t.Helper()
	path := filepath.Join("testdata", "quality_cases.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var cases []qualityCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return cases
}

func validateCaseSchema(t *testing.T, cases []qualityCase) {
	t.Helper()
	seen := map[string]bool{}
	for _, tc := range cases {
		if strings.TrimSpace(tc.ID) == "" {
			t.Fatalf("case missing id")
		}
		if seen[tc.ID] {
			t.Fatalf("duplicate id %q", tc.ID)
		}
		seen[tc.ID] = true
		if strings.TrimSpace(tc.Split) == "" {
			t.Fatalf("%s: missing split", tc.ID)
		}
		if len(tc.Files) == 0 {
			t.Fatalf("%s: files empty", tc.ID)
		}
		if len(tc.Expected) == 0 {
			t.Fatalf("%s: expected empty", tc.ID)
		}
		if tc.CandidateSnapshots == nil {
			t.Fatalf("%s: candidate_snapshots must be non-nil (use [] for empty)", tc.ID)
		}
		if tc.ChapterSnapshots == nil {
			t.Fatalf("%s: chapter_snapshots must be non-nil (use {} for empty)", tc.ID)
		}
		names := map[string]bool{}
		for _, f := range tc.Files {
			if f.Name == "" {
				t.Fatalf("%s: file name empty", tc.ID)
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

// assertNoFrozenWrongAccepts 硬门：不得把 B 参考已知误接受写成 accepted。
func assertNoFrozenWrongAccepts(t *testing.T, cases []qualityCase) {
	t.Helper()
	// 这些 id 对应「错季/错部仍接受」的负例夹具，期望必须是 review/no_candidates
	forbiddenAcceptIDs := map[string]bool{
		"synthetic-kaguya-wrong-season-must-review":    true,
		"synthetic-lupin-must-not-wrong-accept":        true,
		"synthetic-golden-kamuy-must-not-wrong-accept": true,
	}
	for _, tc := range cases {
		if !forbiddenAcceptIDs[tc.ID] {
			continue
		}
		for _, e := range tc.Expected {
			if e.Mode == "accepted" {
				t.Fatalf("%s: 不得把已知误接受场景标为 accepted（got subject_id=%d）", tc.ID, e.SubjectID)
			}
		}
	}
}

func bucketKey(tc qualityCase, exp qualityExpected) string {
	// §8.3 分桶标签（synthetic 粗分）
	tag := "synthetic"
	id := strings.ToLower(tc.ID)
	switch {
	case strings.Contains(id, "xianwang") || strings.Contains(id, "immortal"):
		tag = "季/中文-罗马音-英文"
	case strings.Contains(id, "hxh"):
		tag = "重制年份"
	case strings.Contains(id, "absolute") || strings.Contains(id, "numbering"):
		tag = "绝对编号/不支持编号"
	case strings.Contains(id, "sort"):
		tag = "绝对编号"
	case strings.Contains(id, "dir-only") || strings.Contains(id, "unknown"):
		tag = "纯目录"
	case strings.Contains(id, "kaguya") || strings.Contains(id, "lupin") || strings.Contains(id, "golden"):
		tag = "负例"
	case strings.Contains(id, "no-candidates"):
		tag = "负例"
	}
	return fmt.Sprintf("%s|%s", tag, exp.Mode)
}
