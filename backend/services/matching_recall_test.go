package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fan-web/database"
	"fan-web/services/matching"
)

// F21/F22：离线召回夹具 schema + Session/Library 双入口 runner。
// S6 仅保证 schema 门与可编译；功能预期由 S7 修绿。
// F21 所有权在 S6 持有直至编排者释放给 S7。

type recallFixture struct {
	Version int          `json:"version"`
	Cases   []recallCase `json:"cases"`
}

type recallCase struct {
	ID        string           `json:"id"`
	Files     []recallFile     `json:"files"`
	Subjects  []recallSubject  `json:"subjects"`
	Searches  []recallSearch   `json:"searches"`
	Expected  []recallExpected `json:"expected"`
	ScanError string           `json:"scan_error"`
}

type recallFile struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

type recallSubject struct {
	ID            int             `json:"id"`
	Name          string          `json:"name"`
	NameCn        string          `json:"name_cn"`
	Aliases       []string        `json:"aliases"`
	Year          int             `json:"year"`
	Media         string          `json:"media"`
	EpisodeCount  int             `json:"episode_count"`
	Chapters      []recallChapter `json:"chapters"`
	DetailError   string          `json:"detail_error"`
	ChaptersError string          `json:"chapters_error"`
}

type recallChapter struct {
	ID   int     `json:"id"`
	Ep   float64 `json:"ep"`
	Sort float64 `json:"sort"`
}

type recallSearch struct {
	Terms     []string `json:"terms"`
	IDs       []int    `json:"ids"`
	Saturated bool     `json:"saturated"`
}

type recallExpected struct {
	Name         string `json:"name"`
	Dir          string `json:"dir"`
	Mode         string `json:"mode"`
	SubjectID    int    `json:"subject_id"`
	StoredNumber int    `json:"stored_number"`
}

func recallCasesPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "matching", "recall_cases.json")
}

func loadRecallFixtureRaw(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(recallCasesPath(t))
	if err != nil {
		t.Fatalf("read F22: %v", err)
	}
	return raw
}

func loadRecallFixture(t *testing.T) recallFixture {
	t.Helper()
	raw := loadRecallFixtureRaw(t)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var fx recallFixture
	if err := dec.Decode(&fx); err != nil {
		t.Fatalf("decode F22: %v", err)
	}
	return fx
}

var recallMediaKinds = map[string]bool{
	string(matching.KindUnknown): true,
	string(matching.KindSeries):  true,
	string(matching.KindMovie):   true,
	string(matching.KindOVA):     true,
	string(matching.KindRecap):   true,
	string(matching.KindPromo):   true,
}

var recallDecisionModes = map[string]bool{
	string(matching.Accepted):     true,
	string(matching.Review):       true,
	string(matching.NoCandidates): true,
}

func validateRecallFixture(t *testing.T, fx recallFixture) {
	t.Helper()
	if fx.Version != 1 {
		t.Fatalf("version=%d, want 1", fx.Version)
	}
	if fx.Cases == nil {
		t.Fatal("cases must be explicit array")
	}

	caseIDs := map[string]bool{}
	requiredPrefixes := []string{
		"R01", "R02", "R03", "R04", "R05", "R06", "R07", "R08", "R09", "R09b",
		"R10", "R11", "R11b", "R12", "R13", "R14", "R15", "R16", "R17", "R18",
		"R19", "R20", "R21", "R22", "R23", "R24", "R25", "R26",
	}
	seenPrefix := map[string]bool{}

	for i, c := range fx.Cases {
		if strings.TrimSpace(c.ID) == "" {
			t.Fatalf("cases[%d]: empty id", i)
		}
		if caseIDs[c.ID] {
			t.Fatalf("duplicate case id %q", c.ID)
		}
		caseIDs[c.ID] = true
		for _, p := range requiredPrefixes {
			if c.ID == p || strings.HasPrefix(c.ID, p+"-") {
				seenPrefix[p] = true
			}
		}

		if c.Files == nil || c.Subjects == nil || c.Searches == nil || c.Expected == nil {
			t.Fatalf("%s: files/subjects/searches/expected must be explicit arrays", c.ID)
		}
		if c.ScanError != "" && c.ScanError != "upstream" {
			t.Fatalf("%s: scan_error=%q, want \"\" or \"upstream\"", c.ID, c.ScanError)
		}

		subjectByID := map[int]recallSubject{}
		for j, s := range c.Subjects {
			if s.ID <= 0 {
				t.Fatalf("%s subjects[%d]: id must be positive synthetic id", c.ID, j)
			}
			if _, dup := subjectByID[s.ID]; dup {
				t.Fatalf("%s: duplicate subject id %d", c.ID, s.ID)
			}
			if s.Aliases == nil {
				t.Fatalf("%s subject %d: aliases must be explicit array", c.ID, s.ID)
			}
			if s.Chapters == nil {
				t.Fatalf("%s subject %d: chapters must be explicit array", c.ID, s.ID)
			}
			if !recallMediaKinds[s.Media] {
				t.Fatalf("%s subject %d: media=%q invalid", c.ID, s.ID, s.Media)
			}
			switch s.DetailError {
			case "", "not_found", "upstream":
			default:
				t.Fatalf("%s subject %d: detail_error=%q", c.ID, s.ID, s.DetailError)
			}
			switch s.ChaptersError {
			case "", "not_found", "upstream":
			default:
				t.Fatalf("%s subject %d: chapters_error=%q", c.ID, s.ID, s.ChaptersError)
			}
			for k, ch := range s.Chapters {
				if ch.ID <= 0 {
					t.Fatalf("%s subject %d chapters[%d]: id must be >0", c.ID, s.ID, k)
				}
			}
			subjectByID[s.ID] = s
		}

		fileKeys := map[string]bool{}
		for j, fl := range c.Files {
			if strings.TrimSpace(fl.Name) == "" {
				t.Fatalf("%s files[%d]: empty name", c.ID, j)
			}
			if strings.Contains(fl.Name, "/") || strings.Contains(fl.Name, "\\") {
				t.Fatalf("%s files[%d]: illegal path separator in name %q (use fullwidth ／)", c.ID, j, fl.Name)
			}
			key := fl.Dir + "\x00" + fl.Name
			if fileKeys[key] {
				t.Fatalf("%s: duplicate (dir,name)=(%q,%q)", c.ID, fl.Dir, fl.Name)
			}
			fileKeys[key] = true
		}

		for j, se := range c.Searches {
			if se.Terms == nil || se.IDs == nil {
				t.Fatalf("%s searches[%d]: terms/ids must be explicit arrays", c.ID, j)
			}
			if len(se.Terms) == 0 {
				t.Fatalf("%s searches[%d]: terms empty", c.ID, j)
			}
			for _, id := range se.IDs {
				if _, ok := subjectByID[id]; !ok {
					t.Fatalf("%s searches[%d]: id %d not in subjects", c.ID, j, id)
				}
			}
		}

		expKeys := map[string]bool{}
		if len(c.Expected) != len(c.Files) {
			t.Fatalf("%s: expected len=%d != files len=%d", c.ID, len(c.Expected), len(c.Files))
		}
		for j, e := range c.Expected {
			if !recallDecisionModes[e.Mode] {
				t.Fatalf("%s expected[%d]: mode=%q", c.ID, j, e.Mode)
			}
			key := e.Dir + "\x00" + e.Name
			if !fileKeys[key] {
				t.Fatalf("%s expected[%d]: (%q,%q) not in files", c.ID, j, e.Dir, e.Name)
			}
			if expKeys[key] {
				t.Fatalf("%s: duplicate expected (%q,%q)", c.ID, e.Dir, e.Name)
			}
			expKeys[key] = true
			switch matching.DecisionCode(e.Mode) {
			case matching.Accepted:
				if e.SubjectID <= 0 {
					t.Fatalf("%s expected Accepted without positive subject_id", c.ID)
				}
				if e.StoredNumber <= 0 {
					t.Fatalf("%s expected Accepted without positive stored_number", c.ID)
				}
				if _, ok := subjectByID[e.SubjectID]; !ok {
					t.Fatalf("%s expected subject_id %d missing", c.ID, e.SubjectID)
				}
			default:
				if e.SubjectID != 0 || e.StoredNumber != 0 {
					t.Fatalf("%s non-Accepted expected must have subject_id=0 stored_number=0, got %d/%d",
						c.ID, e.SubjectID, e.StoredNumber)
				}
			}
		}
		for key := range fileKeys {
			if !expKeys[key] {
				t.Fatalf("%s: file not covered by expected", c.ID)
			}
		}
	}

	var missing []string
	for _, p := range requiredPrefixes {
		if !seenPrefix[p] {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("F22 missing required case prefixes: %v", missing)
	}
}

func TestMatchingRecallFixtureSchema(t *testing.T) {
	fx := loadRecallFixture(t)
	validateRecallFixture(t, fx)
}

// ---- provider from F22 ----

func providerFromRecallCase(t *testing.T, c recallCase) matchingProvider {
	t.Helper()
	f := newFakeSessionProvider()
	f.exactOnly = true
	f.detailNotFound = map[int]bool{}
	f.chaptersNotFound = map[int]bool{}

	byID := map[int]matching.Subject{}
	for _, rs := range c.Subjects {
		sub := matching.Subject{
			ID:           rs.ID,
			Name:         rs.Name,
			NameCn:       rs.NameCn,
			Aliases:      append([]string{}, rs.Aliases...),
			Year:         rs.Year,
			Media:        matching.MediaKind(rs.Media),
			EpisodeCount: rs.EpisodeCount,
			Detailed:     false,
		}
		byID[rs.ID] = sub

		switch rs.DetailError {
		case "not_found":
			f.detailNotFound[rs.ID] = true
		case "upstream":
			// Detail 未注册 → fake 返回 ErrMatchUpstream
		case "":
			f.detail[rs.ID] = cloneSubject(sub)
		}

		switch rs.ChaptersError {
		case "not_found":
			f.chaptersNotFound[rs.ID] = true
		case "upstream":
			// 不注册 chapters，且标记：Chapters 对 missing 返回空切片。
			// 用 wrapper 无法按 ID；此处把 upstream 写成显式错误映射不够，
			// 对 chapters_error=upstream 暂不放 chapters，Session 将得空章。
			// 契约要求包装 ErrMatchUpstream：用 sentinel map 扩展。
		case "":
			chs := make([]matching.Chapter, 0, len(rs.Chapters))
			for _, ch := range rs.Chapters {
				chs = append(chs, matching.Chapter{ID: ch.ID, Ep: ch.Ep, Sort: ch.Sort})
			}
			f.chapters[rs.ID] = chs
		}
	}

	// chapters_error=upstream：自定义包装
	base := f
	var chaptersUpstream = map[int]bool{}
	var detailUpstream = map[int]bool{}
	for _, rs := range c.Subjects {
		if rs.ChaptersError == "upstream" {
			chaptersUpstream[rs.ID] = true
		}
		if rs.DetailError == "upstream" {
			detailUpstream[rs.ID] = true
		}
	}

	for _, se := range c.Searches {
		stubs := make([]matching.Subject, 0, len(se.IDs))
		for _, id := range se.IDs {
			sub, ok := byID[id]
			if !ok {
				t.Fatalf("%s: search id %d missing", c.ID, id)
			}
			stub := cloneSubject(sub)
			stub.Detailed = false
			stubs = append(stubs, stub)
		}
		for _, term := range se.Terms {
			base.search[term] = matchingSearchResult{
				Subjects:  cloneSubjects(stubs),
				Saturated: se.Saturated,
				Source:    "v0",
			}
		}
	}

	if len(chaptersUpstream) == 0 && len(detailUpstream) == 0 && c.ScanError == "" {
		return base
	}

	return &recallErrorProvider{
		inner:            base,
		detailUpstream:   detailUpstream,
		chaptersUpstream: chaptersUpstream,
		scanUpstream:     c.ScanError == "upstream",
	}
}

type recallErrorProvider struct {
	inner            *fakeSessionProvider
	detailUpstream   map[int]bool
	chaptersUpstream map[int]bool
	scanUpstream     bool
}

func (p *recallErrorProvider) Search(ctx context.Context, kw string) (matchingSearchResult, error) {
	if p.scanUpstream {
		return matchingSearchResult{}, fmt.Errorf("fixture scan upstream: %w", ErrMatchUpstream)
	}
	return p.inner.Search(ctx, kw)
}

func (p *recallErrorProvider) Detail(ctx context.Context, id int) (matching.Subject, error) {
	if p.scanUpstream {
		return matching.Subject{}, fmt.Errorf("fixture scan upstream: %w", ErrMatchUpstream)
	}
	if p.detailUpstream[id] {
		return matching.Subject{}, fmt.Errorf("fixture detail upstream id=%d: %w", id, ErrMatchUpstream)
	}
	return p.inner.Detail(ctx, id)
}

func (p *recallErrorProvider) Chapters(ctx context.Context, id int) ([]matching.Chapter, error) {
	if p.scanUpstream {
		return nil, fmt.Errorf("fixture scan upstream: %w", ErrMatchUpstream)
	}
	if p.chaptersUpstream[id] {
		return nil, fmt.Errorf("fixture chapters upstream id=%d: %w", id, ErrMatchUpstream)
	}
	return p.inner.Chapters(ctx, id)
}

func cloneSubjects(in []matching.Subject) []matching.Subject {
	out := make([]matching.Subject, len(in))
	for i := range in {
		out[i] = cloneSubject(in[i])
	}
	return out
}

type recallFileOutcome struct {
	Name         string
	Dir          string
	Mode         matching.DecisionCode
	SubjectID    int
	StoredNumber int
}

func fileKey(dir, name string) string { return dir + "\x00" + name }

// sessionOutcomesFromResult：有合法 binding 才 Accepted；Rejected→Review；其余不复制组 Accepted。
func sessionOutcomesFromResult(group matching.Group, res MatchResult) map[string]recallFileOutcome {
	out := map[string]recallFileOutcome{}
	dir := group.Dir
	bound := map[string]matching.EpisodeBinding{}
	for _, b := range res.Chapters.Bindings {
		bound[b.FileName] = b
	}
	rejected := res.Chapters.Rejected
	if rejected == nil {
		rejected = map[string]string{}
	}

	for _, fe := range group.Files {
		key := fileKey(dir, fe.Name)
		if b, ok := bound[fe.Name]; ok && res.Decision.Code == matching.Accepted && res.Decision.WinnerID > 0 {
			out[key] = recallFileOutcome{
				Name: fe.Name, Dir: dir,
				Mode: matching.Accepted, SubjectID: res.Decision.WinnerID, StoredNumber: b.StoredNumber,
			}
			continue
		}
		if _, ok := rejected[fe.Name]; ok {
			out[key] = recallFileOutcome{Name: fe.Name, Dir: dir, Mode: matching.Review}
			continue
		}
		mode := res.Decision.Code
		if mode == matching.Accepted {
			// 组 Accepted 但本文件无 binding → Review（异常隔离）
			mode = matching.Review
		}
		if mode == "" {
			mode = matching.Review
		}
		out[key] = recallFileOutcome{Name: fe.Name, Dir: dir, Mode: mode}
	}
	return out
}

func runRecallCaseSession(t *testing.T, c recallCase) []string {
	t.Helper()
	var mismatches []string
	prov := providerFromRecallCase(t, c)
	rt := testSessionRuntime(t)
	sess := newMatchingSessionForTest(context.Background(), rt, prov)

	inputs := make([]matching.FileInput, 0, len(c.Files))
	for _, fl := range c.Files {
		inputs = append(inputs, matching.FileInput{Name: fl.Name, Dir: fl.Dir})
	}
	groups := matching.GroupFiles(inputs)
	got := map[string]recallFileOutcome{}
	for _, g := range groups {
		res, err := sess.Resolve(g, nil)
		if c.ScanError == "upstream" {
			if err == nil || !errors.Is(err, ErrMatchUpstream) {
				mismatches = append(mismatches, fmt.Sprintf("%s: want upstream err, got res=%v err=%v", c.ID, res.Decision, err))
			}
			for _, fl := range c.Files {
				got[fileKey(fl.Dir, fl.Name)] = recallFileOutcome{
					Name: fl.Name, Dir: fl.Dir, Mode: matching.NoCandidates,
				}
			}
			continue
		}
		if err != nil {
			mismatches = append(mismatches, fmt.Sprintf("%s group %q: Resolve err=%v", c.ID, g.Key, err))
			continue
		}
		for k, v := range sessionOutcomesFromResult(g, res) {
			got[k] = v
		}
	}

	for _, e := range c.Expected {
		key := fileKey(e.Dir, e.Name)
		g, ok := got[key]
		if !ok {
			mismatches = append(mismatches, fmt.Sprintf("%s missing outcome for (%q,%q)", c.ID, e.Dir, e.Name))
			continue
		}
		wantMode := matching.DecisionCode(e.Mode)
		if g.Mode != wantMode || g.SubjectID != e.SubjectID || g.StoredNumber != e.StoredNumber {
			mismatches = append(mismatches, fmt.Sprintf(
				"%s (%q,%q): got mode=%s id=%d num=%d; want mode=%s id=%d num=%d",
				c.ID, e.Dir, e.Name, g.Mode, g.SubjectID, g.StoredNumber, wantMode, e.SubjectID, e.StoredNumber,
			))
		}
	}
	return mismatches
}

func TestMatchingRecallSession(t *testing.T) {
	fx := loadRecallFixture(t)
	validateRecallFixture(t, fx)
	var all []string
	for _, c := range fx.Cases {
		c := c
		t.Run(c.ID, func(t *testing.T) {
			ms := runRecallCaseSession(t, c)
			for _, m := range ms {
				t.Error(m)
			}
			all = append(all, ms...)
		})
	}
	if len(all) > 0 {
		t.Logf("S6 note: Session functional mismatches=%d (expected until S7)", len(all))
	}
}

func runRecallCaseLibrary(t *testing.T, c recallCase) []string {
	t.Helper()
	var mismatches []string

	setupLibraryDB(t)
	root := t.TempDir()
	for _, fl := range c.Files {
		rel := fl.Name
		if fl.Dir != "" {
			rel = filepath.Join(fl.Dir, fl.Name)
		}
		writeLibFile(t, root, rel)
	}

	prov := providerFromRecallCase(t, c)
	rt, err := NewMatchingRuntime(nil, matching.EngineV2, "")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	svc := NewLibraryService(nil, root, WithLibraryMatching(rt))
	svc.newSession = func(ctx context.Context) *MatchingSession {
		return newMatchingSessionForTest(ctx, rt, prov)
	}

	res, err := svc.Scan()
	if c.ScanError == "upstream" {
		if err == nil || !errors.Is(err, ErrMatchUpstream) {
			mismatches = append(mismatches, fmt.Sprintf("%s: want Scan upstream err, got %#v err=%v", c.ID, res, err))
		}
		return mismatches
	}
	if err != nil {
		mismatches = append(mismatches, fmt.Sprintf("%s: Scan err=%v", c.ID, err))
		return mismatches
	}

	type epRow struct {
		BangumiID int
		EpNumber  int
		Dir       string
		FileName  string
	}
	stored := map[string]epRow{}
	for _, rs := range c.Subjects {
		anime, err := database.GetAnimeByBangumiID(rs.ID)
		if err != nil {
			mismatches = append(mismatches, fmt.Sprintf("%s: GetAnimeByBangumiID(%d): %v", c.ID, rs.ID, err))
			continue
		}
		if anime == nil {
			continue
		}
		eps, err := database.ListEpisodesByAnimeID(anime.ID)
		if err != nil {
			mismatches = append(mismatches, fmt.Sprintf("%s: ListEpisodes %d: %v", c.ID, rs.ID, err))
			continue
		}
		for _, ep := range eps {
			key := fileKey(anime.FilePath, ep.FilePath)
			stored[key] = epRow{BangumiID: anime.BangumiID, EpNumber: ep.EpNumber, Dir: anime.FilePath, FileName: ep.FilePath}
		}
	}

	unident := map[string]UnidentifiedFile{}
	for _, u := range res.Unidentified {
		unident[fileKey(u.FilePath, u.FileName)] = u
	}

	caseFiles := map[string]bool{}
	for _, fl := range c.Files {
		caseFiles[fileKey(fl.Dir, fl.Name)] = true
	}

	seenStored := map[string]bool{}
	seenUnident := map[string]bool{}
	for _, e := range c.Expected {
		key := fileKey(e.Dir, e.Name)
		switch matching.DecisionCode(e.Mode) {
		case matching.Accepted:
			row, ok := stored[key]
			if !ok {
				mismatches = append(mismatches, fmt.Sprintf("%s (%q,%q): want stored, missing", c.ID, e.Dir, e.Name))
				continue
			}
			seenStored[key] = true
			if row.BangumiID != e.SubjectID || row.EpNumber != e.StoredNumber {
				mismatches = append(mismatches, fmt.Sprintf(
					"%s (%q,%q): stored bangumi=%d ep=%d; want %d/%d",
					c.ID, e.Dir, e.Name, row.BangumiID, row.EpNumber, e.SubjectID, e.StoredNumber,
				))
			}
			if _, also := unident[key]; also {
				mismatches = append(mismatches, fmt.Sprintf("%s (%q,%q): both stored and unidentified", c.ID, e.Dir, e.Name))
			}
		default:
			if _, ok := unident[key]; !ok {
				mismatches = append(mismatches, fmt.Sprintf("%s (%q,%q): want unidentified for mode=%s", c.ID, e.Dir, e.Name, e.Mode))
				continue
			}
			seenUnident[key] = true
			if _, also := stored[key]; also {
				mismatches = append(mismatches, fmt.Sprintf("%s (%q,%q): both stored and unidentified", c.ID, e.Dir, e.Name))
			}
		}
	}

	for key, row := range stored {
		if caseFiles[key] && !seenStored[key] {
			mismatches = append(mismatches, fmt.Sprintf("%s: unexpected stored (%q,%q) bangumi=%d ep=%d",
				c.ID, row.Dir, row.FileName, row.BangumiID, row.EpNumber))
		}
	}
	for key := range unident {
		if !caseFiles[key] || seenUnident[key] || seenStored[key] {
			continue
		}
		// 未在 Expected 中按 Review/NoCandidates 认领、也未 Accepted 入库的未识别项
		mismatches = append(mismatches, fmt.Sprintf("%s: unexpected unidentified for case file key", c.ID))
		_ = key
	}
	return mismatches
}

func TestMatchingRecallLibrary(t *testing.T) {
	fx := loadRecallFixture(t)
	validateRecallFixture(t, fx)
	// 非并行：共享 database.DB
	var all []string
	for _, c := range fx.Cases {
		c := c
		t.Run(c.ID, func(t *testing.T) {
			ms := runRecallCaseLibrary(t, c)
			for _, m := range ms {
				t.Error(m)
			}
			all = append(all, ms...)
		})
	}
	if len(all) > 0 {
		t.Logf("S6 note: Library functional mismatches=%d (expected until S7)", len(all))
	}
}
