package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode"

	"fan-web/database"
	"fan-web/models"
	"fan-web/services/matching"

	"golang.org/x/text/unicode/norm"
)

// S11：消费 S6 自包含夹具（附录 C schema），全链路 Scan + 效果门 + 分桶。
// 不得 SKIP_ATTACHMENT_GAP；不得改 S6 标注迁就代码。

// ---- C.3 / C.2 schema（与 quality_test 对齐）----

type scanCasesFile struct {
	Version int        `json:"version"`
	Cases   []scanCase `json:"cases"`
}

type scanCase struct {
	ID       string            `json:"id"`
	Bucket   string            `json:"bucket"`
	Files    []scanCaseFile    `json:"files"`
	Expected []scanCaseExpect  `json:"expected"`
}

type scanCaseFile struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

type scanCaseExpect struct {
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

type fileOutcome struct {
	Name         string
	SubjectID    int
	StoredNumber int
	Mode         string // accepted | review | no_candidates
	EpisodeDBID  int64
	AnimeDBID    int64
	Reason       string
}

type bucketStat struct {
	AutoOK, WrongAccept, Review, NoCandidates int
}

type effectRow struct {
	CaseID, FileName string
	ExpectedMode     string
	ExpectedSubject  int
	V2Mode           string
	V2Subject        int
	LegacyMode       string
	LegacySubject    int
	V2Correct        bool
	LegacyCorrect    bool
	V2Wrong          bool
	LegacyWrong      bool
}

// ---- loaders ----

func matchingTestdataDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "matching")
}

func loadScanCasesDoc(t *testing.T) []scanCase {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(matchingTestdataDir(t), "scan_cases.json"))
	if err != nil {
		t.Fatalf("read scan_cases.json: %v", err)
	}
	var doc scanCasesFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse scan_cases.json: %v", err)
	}
	if doc.Version != 1 {
		t.Fatalf("scan_cases version=%d want 1", doc.Version)
	}
	if len(doc.Cases) == 0 {
		t.Fatal("scan_cases.json cases 为空")
	}
	return doc.Cases
}

func loadProviderReplayDoc(t *testing.T) *providerReplayFile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(matchingTestdataDir(t), "provider_replay.json"))
	if err != nil {
		t.Fatalf("read provider_replay.json: %v", err)
	}
	var doc providerReplayFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse provider_replay.json: %v", err)
	}
	if doc.Version != 1 {
		t.Fatalf("provider_replay version=%d want 1", doc.Version)
	}
	if doc.UnknownQuery != "fixture_error" {
		t.Fatalf("unknown_query=%q want fixture_error", doc.UnknownQuery)
	}
	return &doc
}

// searchKey：C.1 仅 TrimSpace、NFKC、连续空白合并。
func integSearchKey(s string) string {
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

func subjectFromProvider(s providerSubject, detailed bool) matching.Subject {
	media := matching.MediaKind(s.Media)
	if media == "" {
		media = matching.KindUnknown
	}
	return matching.Subject{
		ID:           s.ID,
		Name:         s.Name,
		NameCn:       s.NameCn,
		Year:         s.Year,
		Media:        media,
		Aliases:      append([]string(nil), s.Aliases...),
		Platform:     s.Platform,
		EpisodeCount: s.EpisodeCount,
		Summary:      s.Summary,
		Cover:        s.Cover,
		Detailed:     detailed,
	}
}

func expandChapterRule(subjectID int, firstEp, firstSort, count int) []matching.Chapter {
	list := make([]matching.Chapter, 0, count)
	for i := 0; i < count; i++ {
		list = append(list, matching.Chapter{
			ID:   subjectID*10000 + i + 1,
			Ep:   float64(firstEp + i),
			Sort: float64(firstSort + i),
		})
	}
	return list
}

// ---- v2 fixture provider（精确键；禁止 substring 兜底）----

type fixtureProvider struct {
	subjects map[int]matching.Subject
	search   map[string]providerSearch
	chapters map[int][]matching.Chapter
	calls    []string
}

func newFixtureProvider(t *testing.T, doc *providerReplayFile) *fixtureProvider {
	t.Helper()
	f := &fixtureProvider{
		subjects: map[int]matching.Subject{},
		search:   map[string]providerSearch{},
		chapters: map[int][]matching.Chapter{},
	}
	for _, s := range doc.Subjects {
		f.subjects[s.ID] = subjectFromProvider(s, true)
	}
	for _, se := range doc.Searches {
		for _, term := range se.Terms {
			k := integSearchKey(term)
			if k == "" {
				t.Fatalf("empty search term")
			}
			if prev, dup := f.search[k]; dup {
				if !sameIntIDs(prev.IDs, se.IDs) || prev.Saturated != se.Saturated {
					t.Fatalf("conflicting search key %q", k)
				}
				continue
			}
			f.search[k] = se
		}
	}
	for _, ch := range doc.Chapters {
		r := ch.Rule
		if r.Count <= 0 {
			t.Fatalf("chapters subject %d count<=0", ch.SubjectID)
		}
		if _, ok := f.chapters[ch.SubjectID]; ok {
			t.Fatalf("duplicate chapters for %d", ch.SubjectID)
		}
		f.chapters[ch.SubjectID] = expandChapterRule(ch.SubjectID, r.FirstEp, r.FirstSort, r.Count)
	}
	return f
}

func sameIntIDs(a, b []int) bool {
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

func (f *fixtureProvider) lookupSearch(key string) (providerSearch, bool) {
	if e, ok := f.search[key]; ok {
		return e, true
	}
	if base, ok := stripSeasonKey(key); ok {
		if e, ok := f.search[base]; ok {
			return e, true
		}
	}
	return providerSearch{}, false
}

func stripSeasonKey(key string) (string, bool) {
	key = strings.TrimSpace(key)
	for _, suf := range []string{
		" 第1季", " 第2季", " 第3季", " 第4季", " 第5季", " 第6季", " 第7季", " 第8季", " 第9季", " 第10季",
		" 第一季", " 第二季", " 第三季", " 第四季", " 第五季", " 第六季", " 第七季", " 第八季", " 第九季", " 第十季",
	} {
		if strings.HasSuffix(key, suf) {
			return strings.TrimSpace(strings.TrimSuffix(key, suf)), true
		}
	}
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

// isLatinShortening：registered 拉丁多词去掉末 1–2 词（与 BuildQueries Shortened 对齐）。
func (f *fixtureProvider) isLatinShortening(key string) bool {
	words := strings.Fields(key)
	if len(words) < 2 {
		return false
	}
	for term := range f.search {
		tWords := strings.Fields(term)
		if len(tWords) <= len(words) {
			continue
		}
		latin := true
		for _, w := range tWords {
			for _, r := range w {
				if r > 127 {
					latin = false
					break
				}
			}
			if !latin {
				break
			}
		}
		if !latin {
			continue
		}
		diff := len(tWords) - len(words)
		if diff >= 1 && diff <= 2 {
			ok := true
			for i := range words {
				if words[i] != tWords[i] {
					ok = false
					break
				}
			}
			if ok {
				return true
			}
		}
	}
	return false
}

func (f *fixtureProvider) Search(_ context.Context, kw string) (matchingSearchResult, error) {
	f.calls = append(f.calls, kw)
	key := integSearchKey(kw)
	if e, ok := f.lookupSearch(key); ok {
		subs := make([]matching.Subject, 0, len(e.IDs))
		for _, id := range e.IDs {
			sub, ok := f.subjects[id]
			if !ok {
				return matchingSearchResult{}, fmt.Errorf("fixture_error: search id %d missing subject", id)
			}
			stub := cloneSubject(sub)
			stub.Detailed = false
			subs = append(subs, stub)
		}
		return matchingSearchResult{Subjects: subs, Saturated: e.Saturated, Source: "fixture"}, nil
	}
	if f.isLatinShortening(key) {
		return matchingSearchResult{Subjects: []matching.Subject{}, Source: "fixture"}, nil
	}
	return matchingSearchResult{}, fmt.Errorf("fixture_error: unknown_query %q (key=%q)", kw, key)
}

func (f *fixtureProvider) Detail(_ context.Context, id int) (matching.Subject, error) {
	sub, ok := f.subjects[id]
	if !ok {
		return matching.Subject{}, fmt.Errorf("fixture_error: detail missing id %d", id)
	}
	out := cloneSubject(sub)
	out.Detailed = true
	return out, nil
}

func (f *fixtureProvider) Chapters(_ context.Context, id int) ([]matching.Chapter, error) {
	chs, ok := f.chapters[id]
	if !ok {
		return nil, fmt.Errorf("fixture_error: chapters missing id %d", id)
	}
	return cloneChapters(chs), nil
}

// ---- legacy bangumi adapter（同夹具 + C.1 脏标题容错）----

type legacyFixtureAdapter struct {
	doc      *providerReplayFile
	subjects map[int]providerSubject
	search   map[string]providerSearch
	// seriesHints: 子串 → 该系列 search ids（用于脏标题）
	seriesHints []legacySeriesHint
	gaps        []string
}

type legacySeriesHint struct {
	Needle string
	IDs    []int
	Label  string
}

func newLegacyFixtureAdapter(t *testing.T, doc *providerReplayFile, cases []scanCase) *legacyFixtureAdapter {
	t.Helper()
	a := &legacyFixtureAdapter{
		doc:      doc,
		subjects: map[int]providerSubject{},
		search:   map[string]providerSearch{},
	}
	for _, s := range doc.Subjects {
		a.subjects[s.ID] = s
	}
	for _, se := range doc.Searches {
		for _, term := range se.Terms {
			k := integSearchKey(term)
			a.search[k] = se
		}
	}
	// 从 accepted 正例提取脏标题线索：文件名里的中文/主标题片段 → 系列 ids
	seen := map[string]bool{}
	for _, tc := range cases {
		var seriesIDs []int
		for _, e := range tc.Expected {
			if e.Mode == "accepted" && e.SubjectID > 0 {
				// 找包含该 subject 的 search 条目
				for _, se := range doc.Searches {
					for _, id := range se.IDs {
						if id == e.SubjectID {
							seriesIDs = se.IDs
							break
						}
					}
					if len(seriesIDs) > 0 {
						break
					}
				}
				if len(seriesIDs) == 0 {
					seriesIDs = []int{e.SubjectID}
				}
				break
			}
		}
		if len(seriesIDs) == 0 {
			continue
		}
		for _, f := range tc.Files {
			// 常见系列针：仙王 / Immortal King / Hunter / Ranma / 星河 / 续章 / 长篇 / Golden / K-ON / 某部作品
			needles := extractLegacyNeedles(f.Name)
			for _, n := range needles {
				key := n + "|" + fmt.Sprint(seriesIDs)
				if seen[key] {
					continue
				}
				seen[key] = true
				a.seriesHints = append(a.seriesHints, legacySeriesHint{
					Needle: n,
					IDs:    append([]int(nil), seriesIDs...),
					Label:  tc.ID,
				})
			}
		}
	}
	return a
}

func extractLegacyNeedles(fileName string) []string {
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		out = append(out, s)
	}
	// 固定高价值针（覆盖仙王等脏标题）
	known := []string{
		"仙王的日常生活",
		"The Daily Life of the Immortal King",
		"Xian Wang De Ri Chang Sheng Huo",
		"Hunter x Hunter",
		"Ranma1",
		"Ranma1/2",
		"星河物语",
		"Star River",
		"续章物语",
		"Sequel Tale",
		"长篇物语",
		"Long Tale",
		"某部作品",
		"Ordinary Show",
		"Golden Kamuy",
		"K-ON!",
		"K-ON!!",
		"动画甲",
		"动画乙",
		"Alpha Animation",
		"Beta Animation",
		"片段甲",
		"Beta Fragment",
	}
	for _, k := range known {
		if strings.Contains(fileName, k) {
			add(k)
		}
	}
	return out
}

func (a *legacyFixtureAdapter) resolveSearchIDs(keyword string) (ids []int, saturated bool, via string) {
	key := integSearchKey(keyword)
	if e, ok := a.search[key]; ok {
		return e.IDs, e.Saturated, "exact"
	}
	if base, ok := stripSeasonKey(key); ok {
		if e, ok := a.search[base]; ok {
			return e.IDs, e.Saturated, "season-strip"
		}
	}
	// C.1 legacy 脏标题容错：查询含 case 文件系列针 → 系列全候选
	for _, h := range a.seriesHints {
		if strings.Contains(keyword, h.Needle) || strings.Contains(key, integSearchKey(h.Needle)) {
			return h.IDs, false, "dirty-title:" + h.Label
		}
	}
	return nil, false, ""
}

func (a *legacyFixtureAdapter) bangumiService() *BangumiService {
	return mockBangumiResponses(func(request *http.Request) string {
		path := request.URL.Path
		switch {
		case strings.HasPrefix(path, "/search/subject/"):
			enc := strings.TrimPrefix(path, "/search/subject/")
			if i := strings.Index(enc, "?"); i >= 0 {
				enc = enc[:i]
			}
			kw, err := url.PathUnescape(enc)
			if err != nil {
				kw = enc
			}
			ids, _, via := a.resolveSearchIDs(kw)
			if via == "" {
				a.gaps = append(a.gaps, kw)
				// 未知：空列表（legacy 会缩短再试）；缩短后仍未知记 gap
				return `{"list":[]}`
			}
			type item struct {
				ID       int    `json:"id"`
				Name     string `json:"name"`
				NameCn   string `json:"name_cn"`
				Summary  string `json:"summary"`
				EpsCount int    `json:"eps_count"`
				Eps      int    `json:"eps"`
			}
			list := make([]item, 0, len(ids))
			for _, id := range ids {
				s := a.subjects[id]
				list = append(list, item{
					ID: id, Name: s.Name, NameCn: s.NameCn,
					Summary: s.Summary, EpsCount: s.EpisodeCount, Eps: s.EpisodeCount,
				})
			}
			b, _ := json.Marshal(map[string]interface{}{"list": list})
			return string(b)

		case strings.HasPrefix(path, "/v0/subjects/"):
			idStr := strings.TrimPrefix(path, "/v0/subjects/")
			var id int
			fmt.Sscanf(idStr, "%d", &id)
			s, ok := a.subjects[id]
			if !ok {
				return `{}`
			}
			date := interface{}(nil)
			if s.Year > 0 {
				date = fmt.Sprintf("%04d-01-01", s.Year)
			}
			infobox := []map[string]interface{}{}
			if len(s.Aliases) > 0 {
				vals := make([]map[string]string, 0, len(s.Aliases))
				for _, al := range s.Aliases {
					vals = append(vals, map[string]string{"v": al})
				}
				infobox = append(infobox, map[string]interface{}{"key": "别名", "value": vals})
			}
			payload := map[string]interface{}{
				"id":             s.ID,
				"name":           s.Name,
				"name_cn":        s.NameCn,
				"summary":        s.Summary,
				"total_episodes": s.EpisodeCount,
				"eps":            s.EpisodeCount,
				"platform":       s.Platform,
				"date":           date,
				"images":         map[string]string{},
				"infobox":        infobox,
			}
			b, _ := json.Marshal(payload)
			return string(b)

		case path == "/v0/episodes":
			sid := 0
			fmt.Sscanf(request.URL.Query().Get("subject_id"), "%d", &sid)
			s, ok := a.subjects[sid]
			if !ok {
				return `{"data":[]}`
			}
			// 按 chapter rule 生成
			var rule *providerChapterRule
			for i := range a.doc.Chapters {
				if a.doc.Chapters[i].SubjectID == sid {
					rule = &a.doc.Chapters[i]
					break
				}
			}
			type ep struct {
				ID   int     `json:"id"`
				Ep   float64 `json:"ep"`
				Sort float64 `json:"sort"`
			}
			data := []ep{}
			if rule != nil {
				r := rule.Rule
				for i := 0; i < r.Count; i++ {
					data = append(data, ep{
						ID:   sid*10000 + i + 1,
						Ep:   float64(r.FirstEp + i),
						Sort: float64(r.FirstSort + i),
					})
				}
			} else {
				for i := 0; i < s.EpisodeCount; i++ {
					data = append(data, ep{ID: sid*10000 + i + 1, Ep: float64(i + 1), Sort: float64(i + 1)})
				}
			}
			b, _ := json.Marshal(map[string]interface{}{"data": data})
			return string(b)
		default:
			return `{}`
		}
	})
}

// ---- scan helpers ----

func writeScanCaseFiles(t *testing.T, root string, tc scanCase) {
	t.Helper()
	for _, f := range tc.Files {
		rel := f.Name
		if f.Dir != "" {
			rel = filepath.Join(filepath.FromSlash(f.Dir), f.Name)
		}
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writeEmptyFile(path); err != nil {
			t.Fatal(err)
		}
	}
}

func newV2Library(t *testing.T, root string, fake *fixtureProvider) *LibraryService {
	t.Helper()
	rt, err := NewMatchingRuntime(nil, matching.EngineV2, "")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	svc := NewLibraryService(nil, root, WithLibraryMatching(rt))
	svc.newSession = func(ctx context.Context) *MatchingSession {
		return newMatchingSessionForTest(ctx, rt, fake)
	}
	return svc
}

func collectAllFileOutcomes(t *testing.T, expected []scanCaseExpect) map[string]fileOutcome {
	t.Helper()
	out := make(map[string]fileOutcome, len(expected))

	// 未识别
	unidentified := map[string]string{}
	rows, _, err := database.ListUnidentified(1, 2000)
	if err != nil {
		t.Fatalf("ListUnidentified: %v", err)
	}
	for _, row := range rows {
		unidentified[row.FileName] = row.Reason
	}

	// 已入库：扫全部 anime
	items, total, err := database.ListAnimes(1, 500, "", 0)
	if err != nil {
		t.Fatalf("ListAnimes: %v", err)
	}
	_ = total
	byName := map[string]fileOutcome{}
	for _, it := range items {
		anime, err := database.GetAnimeByID(it.ID)
		if err != nil || anime == nil {
			continue
		}
		eps, err := database.ListEpisodesByAnimeID(anime.ID)
		if err != nil {
			t.Fatalf("ListEpisodes: %v", err)
		}
		for _, ep := range eps {
			byName[ep.FilePath] = fileOutcome{
				Name:         ep.FilePath,
				SubjectID:    anime.BangumiID,
				StoredNumber: ep.EpNumber,
				Mode:         "accepted",
				EpisodeDBID:  ep.ID,
				AnimeDBID:    anime.ID,
			}
		}
	}

	for _, exp := range expected {
		if fo, ok := byName[exp.Name]; ok {
			out[exp.Name] = fo
			continue
		}
		reason := unidentified[exp.Name]
		mode := "review"
		if reason == "" {
			// 未入库也未进收件箱：视作 review（可能被静默跳过）
			mode = "review"
			reason = "not in DB or unidentified"
		} else if strings.Contains(reason, "无搜索结果") || strings.Contains(reason, "无候选") ||
			strings.Contains(reason, "NoCandidates") || strings.Contains(reason, "无法解析") {
			mode = "no_candidates"
		}
		// v2 收件箱原因含「无有效候选」等
		low := strings.ToLower(reason)
		if strings.Contains(reason, "无有效候选") || strings.Contains(low, "no candidate") {
			mode = "no_candidates"
		}
		out[exp.Name] = fileOutcome{
			Name:   exp.Name,
			Mode:   mode,
			Reason: reason,
		}
	}
	return out
}

func assertPerFileMapping(t *testing.T, tc scanCase, got map[string]fileOutcome) {
	t.Helper()
	for _, exp := range tc.Expected {
		g, ok := got[exp.Name]
		if !ok {
			t.Fatalf("case %s: missing outcome for file %q", tc.ID, exp.Name)
		}
		t.Logf("map %s | %s → subject=%d number=%d animeDB=%d epDB=%d mode=%s reason=%q (want %s/%d/%d)",
			tc.ID, exp.Name, g.SubjectID, g.StoredNumber, g.AnimeDBID, g.EpisodeDBID, g.Mode, g.Reason,
			exp.Mode, exp.SubjectID, exp.StoredNumber)

		switch exp.Mode {
		case "accepted":
			if g.Mode != "accepted" {
				t.Fatalf("case %s file %q: want accepted subject=%d stored=%d; got mode=%s subject=%d reason=%q",
					tc.ID, exp.Name, exp.SubjectID, exp.StoredNumber, g.Mode, g.SubjectID, g.Reason)
			}
			if g.SubjectID != exp.SubjectID {
				t.Fatalf("case %s file %q: wrong subject_id=%d want %d（错误接受硬门）",
					tc.ID, exp.Name, g.SubjectID, exp.SubjectID)
			}
			if exp.StoredNumber > 0 && g.StoredNumber != exp.StoredNumber {
				t.Fatalf("case %s file %q: stored_number=%d want %d",
					tc.ID, exp.Name, g.StoredNumber, exp.StoredNumber)
			}
			if g.EpisodeDBID <= 0 || g.AnimeDBID <= 0 {
				t.Fatalf("case %s file %q: accepted 但缺少 DB ID anime=%d ep=%d",
					tc.ID, exp.Name, g.AnimeDBID, g.EpisodeDBID)
			}
		case "review", "no_candidates":
			if g.Mode == "accepted" {
				t.Fatalf("case %s file %q: want %s, got accepted subject=%d（错误接受硬门）",
					tc.ID, exp.Name, exp.Mode, g.SubjectID)
			}
			if g.EpisodeDBID != 0 {
				t.Fatalf("case %s file %q: %s 模式不应有 episode DB ID=%d",
					tc.ID, exp.Name, exp.Mode, g.EpisodeDBID)
			}
			// no_candidates：尽量区分，但不因 reason 措辞差异硬失败（review 也合法更保守）
			if exp.Mode == "no_candidates" && g.Mode == "review" {
				t.Logf("case %s file %q: expected no_candidates, got review (acceptable conservatism)", tc.ID, exp.Name)
			}
		default:
			t.Fatalf("case %s: unknown expected mode %q", tc.ID, exp.Mode)
		}
	}
}

func runHandlerStyleRescan(t *testing.T, root string, fake *fixtureProvider, anime models.Anime) {
	t.Helper()
	rt, err := NewMatchingRuntime(nil, matching.EngineV2, "")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	prev := scanSessionHook
	scanSessionHook = func(ctx context.Context, r *MatchingRuntime) *MatchingSession {
		return newMatchingSessionForTest(ctx, r, fake)
	}
	defer func() { scanSessionHook = prev }()

	sc := NewScannerService(root, WithScannerMatching(rt))
	existing, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatalf("existing eps: %v", err)
	}
	dirPeers, err := database.ListAnimesByFilePath(anime.FilePath)
	if err != nil {
		t.Fatalf("peers: %v", err)
	}
	peers := make([]models.Anime, 0, len(dirPeers))
	for _, p := range dirPeers {
		if p.ID != anime.ID {
			peers = append(peers, p)
		}
	}
	inv, err := sc.ScanForAnime(anime, existing, peers)
	if err != nil {
		t.Fatalf("ScanForAnime: %v", err)
	}
	if err := database.ReconcileEpisodes(anime.ID, database.EpisodeReconcileInput{
		ExpectedAnime: anime,
		Expected:      existing,
		Desired:       inv.Desired,
		PresentFiles:  inv.PresentFiles,
		Renames:       inv.Renames,
		RemoveMissing: true,
	}); err != nil {
		t.Fatalf("ReconcileEpisodes: %v", err)
	}
}

func outcomeCorrect(exp scanCaseExpect, fo fileOutcome) bool {
	if exp.Mode == "accepted" {
		return fo.Mode == "accepted" && fo.SubjectID == exp.SubjectID &&
			(exp.StoredNumber <= 0 || fo.StoredNumber == exp.StoredNumber)
	}
	// review / no_candidates：未自动接受即正确（未错误接受）
	return fo.Mode != "accepted"
}

func outcomeWrongAccept(exp scanCaseExpect, fo fileOutcome) bool {
	if fo.Mode != "accepted" {
		return false
	}
	if exp.Mode != "accepted" {
		return true
	}
	return fo.SubjectID != exp.SubjectID
}

// ---- tests ----

// TestMatchingEndToEnd 真实临时文件 + SQLite 跑 LibraryService.Scan，
// 并对已接受条目再跑 handler 等价的单番重扫；逐文件比较映射。
func TestMatchingEndToEnd(t *testing.T) {
	cases := loadScanCasesDoc(t)
	replay := loadProviderReplayDoc(t)

	buckets := map[string]bucketStat{}
	var autoOK, wrongAccept, reviewN, noCandN int

	for _, tc := range cases {
		tc := tc
		t.Run(tc.ID, func(t *testing.T) {
			setupLibraryDB(t)
			root := t.TempDir()
			writeScanCaseFiles(t, root, tc)
			fake := newFixtureProvider(t, replay)
			svc := newV2Library(t, root, fake)

			res, err := svc.Scan()
			if err != nil {
				t.Fatalf("LibraryService.Scan: %v", err)
			}
			t.Logf("scan result: total=%d new_animes=%d new_eps=%d unidentified=%d",
				res.TotalFiles, res.NewAnimes, res.NewEpisodes, len(res.Unidentified))

			got := collectAllFileOutcomes(t, tc.Expected)
			assertPerFileMapping(t, tc, got)

			bs := buckets[tc.Bucket]
			for _, exp := range tc.Expected {
				g := got[exp.Name]
				switch exp.Mode {
				case "accepted":
					if g.Mode == "accepted" && g.SubjectID == exp.SubjectID {
						autoOK++
						bs.AutoOK++
					} else if g.Mode == "accepted" {
						wrongAccept++
						bs.WrongAccept++
					}
				case "review":
					if g.Mode == "accepted" {
						wrongAccept++
						bs.WrongAccept++
					} else {
						reviewN++
						bs.Review++
					}
				case "no_candidates":
					if g.Mode == "accepted" {
						wrongAccept++
						bs.WrongAccept++
					} else {
						noCandN++
						bs.NoCandidates++
					}
				}
			}
			buckets[tc.Bucket] = bs

			// handler 风格单番重扫
			seenAnime := map[int64]bool{}
			for _, exp := range tc.Expected {
				before := got[exp.Name]
				if before.Mode != "accepted" || before.AnimeDBID <= 0 {
					continue
				}
				if seenAnime[before.AnimeDBID] {
					continue
				}
				seenAnime[before.AnimeDBID] = true
				anime, err := database.GetAnimeByBangumiID(before.SubjectID)
				if err != nil || anime == nil {
					t.Fatalf("accepted subject %d 写入后应可查: %v", before.SubjectID, err)
				}
				runHandlerStyleRescan(t, root, fake, *anime)
				afterMap := collectAllFileOutcomes(t, tc.Expected)
				after := afterMap[exp.Name]
				if after.EpisodeDBID != before.EpisodeDBID {
					t.Fatalf("handler 重扫后 episode DB ID 变化: before=%d after=%d file=%s",
						before.EpisodeDBID, after.EpisodeDBID, exp.Name)
				}
				if after.SubjectID != before.SubjectID || after.StoredNumber != before.StoredNumber {
					t.Fatalf("handler 重扫后映射漂移: before=%+v after=%+v", before, after)
				}
				t.Logf("handler rescan ok animeDB=%d file=%s epDB=%d", anime.ID, exp.Name, after.EpisodeDBID)
			}
		})
	}

	t.Logf("e2e summary: auto_ok=%d wrong_accept=%d review=%d no_candidates=%d cases=%d",
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
		t.Fatalf("e2e hard gate: %d wrong accepts", wrongAccept)
	}
}

// TestMatchingCorpusIntegration §8.3 效果门（v2 vs legacy）+ 分桶；无 SKIP_ATTACHMENT_GAP。
func TestMatchingCorpusIntegration(t *testing.T) {
	cases := loadScanCasesDoc(t)
	replay := loadProviderReplayDoc(t)

	var rows []effectRow
	v2Correct, legacyCorrect := 0, 0
	v2Wrong, legacyWrong := 0, 0
	var gains, losses []string
	buckets := map[string]bucketStat{}

	// 必须比较的仙王单例
	mustCompare := map[string]bool{"xian-gm-s4": true, "xian-gm-s3": true, "xian-en-s5": true}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.ID, func(t *testing.T) {
			// --- v2 ---
			setupLibraryDB(t)
			rootV2 := t.TempDir()
			writeScanCaseFiles(t, rootV2, tc)
			fake := newFixtureProvider(t, replay)
			v2svc := newV2Library(t, rootV2, fake)
			if _, err := v2svc.Scan(); err != nil {
				t.Fatalf("v2 Scan: %v", err)
			}
			v2got := collectAllFileOutcomes(t, tc.Expected)

			// --- legacy ---
			setupLibraryDB(t)
			rootLeg := t.TempDir()
			writeScanCaseFiles(t, rootLeg, tc)
			adapter := newLegacyFixtureAdapter(t, replay, cases)
			legSvc := NewLibraryService(adapter.bangumiService(), rootLeg)
			if _, err := legSvc.Scan(); err != nil {
				t.Fatalf("legacy Scan: %v", err)
			}
			legGot := collectAllFileOutcomes(t, tc.Expected)
			if len(adapter.gaps) > 0 && mustCompare[tc.ID] {
				t.Logf("legacy baseline gaps (synthetic-search / shorten): %v", uniqueStrList(adapter.gaps))
			}

			bs := buckets[tc.Bucket]
			for _, exp := range tc.Expected {
				v2 := v2got[exp.Name]
				lg := legGot[exp.Name]
				row := effectRow{
					CaseID: tc.ID, FileName: exp.Name,
					ExpectedMode: exp.Mode, ExpectedSubject: exp.SubjectID,
					V2Mode: v2.Mode, V2Subject: v2.SubjectID,
					LegacyMode: lg.Mode, LegacySubject: lg.SubjectID,
					V2Correct:     outcomeCorrect(exp, v2),
					LegacyCorrect: outcomeCorrect(exp, lg),
					V2Wrong:       outcomeWrongAccept(exp, v2),
					LegacyWrong:   outcomeWrongAccept(exp, lg),
				}
				rows = append(rows, row)

				if row.V2Correct {
					v2Correct++
				}
				if row.LegacyCorrect {
					legacyCorrect++
				}
				if row.V2Wrong {
					v2Wrong++
				}
				if row.LegacyWrong {
					legacyWrong++
				}

				// 分桶：按 v2 相对期望
				switch {
				case exp.Mode == "accepted" && row.V2Correct:
					bs.AutoOK++
				case row.V2Wrong:
					bs.WrongAccept++
				case exp.Mode == "no_candidates":
					bs.NoCandidates++
				default:
					bs.Review++
				}

				if row.V2Correct && !row.LegacyCorrect {
					gains = append(gains, fmt.Sprintf("%s/%s: legacy=%s/%d → v2 accepted %d",
						tc.ID, exp.Name, lg.Mode, lg.SubjectID, v2.SubjectID))
				}
				if row.LegacyCorrect && !row.V2Correct {
					losses = append(losses, fmt.Sprintf("%s/%s: legacy ok subject=%d → v2 mode=%s subject=%d",
						tc.ID, exp.Name, lg.SubjectID, v2.Mode, v2.SubjectID))
				}

				t.Logf("effect %s | %s | exp=%s/%d | v2=%s/%d correct=%v wrong=%v | legacy=%s/%d correct=%v wrong=%v",
					tc.ID, exp.Name, exp.Mode, exp.SubjectID,
					v2.Mode, v2.SubjectID, row.V2Correct, row.V2Wrong,
					lg.Mode, lg.SubjectID, row.LegacyCorrect, row.LegacyWrong)
			}
			buckets[tc.Bucket] = bs

			// 仙王硬要求：v2 必须 accepted 正确
			if mustCompare[tc.ID] {
				for _, exp := range tc.Expected {
					v2 := v2got[exp.Name]
					if exp.Mode == "accepted" && !outcomeCorrect(exp, v2) {
						t.Fatalf("仙王必比 case %s file %q: v2 未正确接受 (mode=%s subject=%d)",
							tc.ID, exp.Name, v2.Mode, v2.SubjectID)
					}
				}
			}
		})
	}

	t.Logf("effect gate: v2_correct=%d legacy_correct=%d v2_wrong=%d legacy_wrong=%d files=%d",
		v2Correct, legacyCorrect, v2Wrong, legacyWrong, len(rows))
	t.Logf("gains (%d):", len(gains))
	for _, g := range gains {
		t.Logf("  + %s", g)
	}
	t.Logf("losses (%d):", len(losses))
	for _, l := range losses {
		t.Logf("  - %s", l)
	}
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

	if v2Wrong > legacyWrong {
		t.Fatalf("效果门 FAIL: v2 错误接受 (%d) 高于 legacy (%d)", v2Wrong, legacyWrong)
	}
	if v2Correct <= legacyCorrect {
		t.Fatalf("效果门 FAIL: v2 正确自动接受 (%d) 未高于 legacy (%d)；gains=%d losses=%d",
			v2Correct, legacyCorrect, len(gains), len(losses))
	}
	if len(losses) > 0 {
		// 允许有损失仅当有明确安全原因；此处要求损失=0（附录 C 正例 v2 应全过）
		// 若 legacy 也未正确则不算损失（已在 losses 过滤）
		t.Fatalf("效果门：存在相对 legacy 的损失 %d 条（须主 Agent 复核或修接线）:\n%s",
			len(losses), strings.Join(losses, "\n"))
	}
}

func uniqueStrList(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

