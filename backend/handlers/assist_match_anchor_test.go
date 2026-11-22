package handlers

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"fan-web/database"
	"fan-web/models"
	"fan-web/services/matching"
	"fan-web/utils"
)

// 分割季夹具（同 Re:Zero 第四季结构）：第二季上半 9101 ep1..11 / sort 25..35，下半 9102 ep1..8 / sort 36..43。
// 文件按季内连续编号 2nd Season - 12..19：原号无法直接命中任何章节，需季锚点 25 → sort 36..43 = 下半 ep1..8。
func splitCourSeeds() []bangumiSeed {
	chs := func(id, n, firstSort int) []matching.Chapter {
		out := make([]matching.Chapter, 0, n)
		for i := 1; i <= n; i++ {
			out = append(out, matching.Chapter{ID: id*100 + i, Ep: float64(i), Sort: float64(firstSort + i - 1)})
		}
		return out
	}
	return []bangumiSeed{
		{
			Subject: matching.Subject{
				ID: 9101, Name: "Kaiju Story 2nd Season", NameCn: "怪兽物语 第二季",
				Year: 2025, Media: matching.KindSeries, EpisodeCount: 11, Detailed: true,
			},
			SearchKeys: []string{"Kaiju Story"},
			Chapters:   chs(9101, 11, 25),
		},
		{
			Subject: matching.Subject{
				ID: 9102, Name: "Kaiju Story 2nd Season Part 2", NameCn: "怪兽物语 第二季 第2部分",
				Year: 2026, Media: matching.KindSeries, EpisodeCount: 8, Detailed: true,
			},
			SearchKeys: []string{"Kaiju Story"},
			Chapters:   chs(9102, 8, 36),
		},
	}
}

// PR #20 复审问题 3：title_hint/无 hint 路径的预览经季锚点把文件号 12..19 映射为 9102 第 1..8 集；
// 同一请求正式入库必须沿用该映射（旧实现在受信任提交时重新解释章节、丢掉季锚点而返回 review_required）。
func TestAssistHintCommitUsesPreviewSeasonAnchorBindings(t *testing.T) {
	root := t.TempDir()
	relDir := "Kaiju/S2"
	names := make([]string, 0, 8)
	for ep := 12; ep <= 19; ep++ {
		names = append(names, fmt.Sprintf("[Group] Kaiju Story 2nd Season - %02d [1080p].mkv", ep))
	}
	writeVideos(t, filepath.Join(root, relDir), names...)
	handler := setupScanHandlerV2(t, root, splitCourSeeds()...)

	preview := postAssist(t, handler, models.AssistMatchRequest{FilePath: relDir, FileNames: names, AutoCommit: boolPtr(false)})
	if preview.Code != utils.CodeSuccess {
		t.Fatalf("预览应成功: %d %q", preview.Code, preview.Message)
	}
	var pv models.AssistMatchResponse
	if err := json.Unmarshal(preview.Data, &pv); err != nil {
		t.Fatal(err)
	}
	if pv.Status != assistStatusMatched || pv.Candidate == nil || pv.Candidate.ID != 9102 || len(pv.Bindings) != len(names) {
		t.Fatalf("前提：预览应经季锚点匹配 9102 并给出 8 个绑定: %s", preview.Data)
	}

	commit := postAssist(t, handler, models.AssistMatchRequest{FilePath: relDir, FileNames: names})
	if commit.Code != utils.CodeSuccess {
		t.Fatalf("提交应成功: %d %q %s", commit.Code, commit.Message, commit.Data)
	}
	var cm models.AssistMatchResponse
	if err := json.Unmarshal(commit.Data, &cm); err != nil {
		t.Fatal(err)
	}
	if cm.Status != assistStatusCommitted || cm.Anime == nil || cm.Anime.BangumiID != 9102 {
		t.Fatalf("预览成功的同一批文件应正式入库 9102: %s", commit.Data)
	}
	eps, err := database.ListEpisodesByAnimeID(cm.Anime.ID)
	if err != nil || len(eps) != len(names) {
		t.Fatalf("应入库 %d 集: %v %#v", len(names), err, eps)
	}
	byPath := map[string]int{}
	for _, ep := range eps {
		byPath[ep.FilePath] = ep.EpNumber
	}
	for i, name := range names {
		want := pv.Bindings[i].EpNumber
		if pv.Bindings[i].FileName != name || want != i+1 || byPath[name] != want {
			t.Fatalf("%s: 入库 ep_number=%d，预览=%+v，want %d", name, byPath[name], pv.Bindings[i], i+1)
		}
	}
}

// 仙王的日常生活 第三～五季（条目、话数与章节规则取自 services/testdata/matching/provider_replay.json）。
func xianWangSeeds() []bangumiSeed {
	out := make([]bangumiSeed, 0, 3)
	for _, s := range []struct {
		id, year int
		name     string
	}{
		{358742, 2022, "仙王的日常生活 第三季"},
		{405214, 2023, "仙王的日常生活 第四季"},
		{529875, 2025, "仙王的日常生活 第五季"},
	} {
		out = append(out, bangumiSeed{
			Subject: matching.Subject{
				ID: s.id, Name: s.name, NameCn: s.name, Year: s.year,
				Media: matching.KindSeries, EpisodeCount: 12, Platform: "WEB", Detailed: true,
			},
			SearchKeys: []string{"仙王的日常生活"},
			Chapters:   seriesChapters(s.id, 12),
		})
	}
	return out
}

// PR #20 复审问题 2：第五季第 3 集与 4.5 集同批预览，4.5 无有效绑定 → 预览须 review_required（旧实现报 matched
// 且只给出第 3 集绑定）；同批提交同样 review_required 且零写入番剧/剧集。
func TestAssistHintPreviewPartialBindingsReviewRequired(t *testing.T) {
	root := t.TempDir()
	relDir := "XianWang/S5"
	names := []string{
		"[GM-Team][国漫][仙王的日常生活 第5季][Xian Wang De Ri Chang Sheng Huo Ⅴ][2025][03][AVC][GB][1080P].mp4",
		"[GM-Team][国漫][仙王的日常生活 第5季][Xian Wang De Ri Chang Sheng Huo Ⅴ][2025][04.5][AVC][GB][1080P].mp4",
	}
	writeVideos(t, filepath.Join(root, relDir), names...)
	handler := setupScanHandlerV2(t, root, xianWangSeeds()...)

	decode := func(resp confirmResponse) models.AssistMatchResponse {
		t.Helper()
		if resp.Code != utils.CodeSuccess {
			t.Fatalf("应 code=0: %d %q %s", resp.Code, resp.Message, resp.Data)
		}
		var out models.AssistMatchResponse
		if err := json.Unmarshal(resp.Data, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	pv := decode(postAssist(t, handler, models.AssistMatchRequest{FilePath: relDir, FileNames: names, AutoCommit: boolPtr(false)}))
	if pv.Status != assistStatusReviewRequired || pv.Candidate != nil || len(pv.Bindings) != 0 {
		t.Fatalf("部分文件无绑定时预览不得报 matched: %+v", pv)
	}
	if len(pv.Candidates) != 1 || pv.Candidates[0].ID != 529875 {
		t.Fatalf("预览复核应给出第五季候选: %+v", pv.Candidates)
	}

	cm := decode(postAssist(t, handler, models.AssistMatchRequest{FilePath: relDir, FileNames: names}))
	if cm.Status != assistStatusReviewRequired {
		t.Fatalf("提交同批文件应 review_required: %+v", cm)
	}
	if n := countAnimesByBangumiID(t, 529875); n != 0 {
		t.Fatalf("复核不得建档，got %d", n)
	}
	var eps int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM episodes").Scan(&eps); err != nil || eps != 0 {
		t.Fatalf("复核不得写剧集: %v %d", err, eps)
	}

	// 只含第 3 集时仍 matched 并绑定 ep3。
	one := decode(postAssist(t, handler, models.AssistMatchRequest{FilePath: relDir, FileNames: names[:1], AutoCommit: boolPtr(false)}))
	if one.Status != assistStatusMatched || one.Candidate == nil || one.Candidate.ID != 529875 || len(one.Bindings) != 1 || one.Bindings[0].EpNumber != 3 {
		t.Fatalf("单文件预览应 matched ep3: %+v", one)
	}
}
