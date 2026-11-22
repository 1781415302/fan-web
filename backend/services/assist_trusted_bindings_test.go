package services

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"fan-web/models"
	"fan-web/services/matching"
)

// PR #20 复审问题 3（Re:Zero 回放）：辅助匹配预览（ResolveAssistByHint）按季锚点把 4th - 12..19 映射为
// 夺还篇 633836 ep1..8；受信任提交沿用同一次预览的绑定（WithTrustedBindings）后入库结果与预览一致，
// 而不带绑定的受信任重解释会丢掉季锚点（review_required）。
func TestAssistTrustedCommitWithPreviewBindingsRe0Replay(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	dir := filepath.Join("Re0", "Season4")
	names := sceneARe0Files(false)
	writeScanVideos(t, filepath.Join(root, dir), names...)
	fake := newFixtureProvider(t, loadProviderReplayDoc(t))
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })
	prev := scanSessionHook
	scanSessionHook = func(ctx context.Context, r *MatchingRuntime) *MatchingSession {
		return newMatchingSessionForTest(ctx, r, fake)
	}
	t.Cleanup(func() { scanSessionHook = prev })

	preview, err := newV2Scanner(t, root).ResolveAssistByHint(context.Background(), AssistHintInput{RelDir: filepath.ToSlash(dir), FileNames: names})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Decision.Code != matching.Accepted || preview.Subject == nil || preview.Subject.ID != 633836 || len(preview.Chapters.Bindings) != len(names) {
		t.Fatalf("前提：预览应接受夺还篇并绑定 8 个文件: %+v", preview.Decision)
	}

	anime := createConfirmTarget(t, fake, 633836, dir)
	if _, err := scanTargetAndReconcile(t, root, fake, anime, WithOnlyFiles(names), WithTrustedTarget()); !errors.Is(err, ErrMatchReviewRequired) {
		t.Fatalf("前提：不带预览绑定的受信任重解释应 review_required，got %v", err)
	}
	if _, err := scanTargetAndReconcile(t, root, fake, anime, WithOnlyFiles(names), WithTrustedTarget(), WithTrustedBindings(preview.Chapters.Bindings)); err != nil {
		t.Fatalf("沿用预览绑定提交应成功: %v", err)
	}
	got := snapshotAnimeEpisodes(t, anime.ID)
	for i, name := range names {
		if got[name].EpNumber != i+1 {
			t.Fatalf("%s: ep_number=%d want %d (got=%v)", name, got[name].EpNumber, i+1, got)
		}
	}
}

// 预览绑定不完整（缺文件 / 重复 / 非正集号）时整体 review_required，不静默跳过。
func TestPresetTrustedBindingsRequiresEveryFile(t *testing.T) {
	anime := animeForPreset()
	files := []string{"a - 12.mkv", "a - 13.mkv"}
	ok := []matching.EpisodeBinding{{FileName: "a - 12.mkv", StoredNumber: 1}, {FileName: "a - 13.mkv", StoredNumber: 2}}
	got, err := presetTrustedBindings(anime, files, ok, 0)
	if err != nil || len(got) != 2 || got[0].StoredNumber != 1 || got[1].StoredNumber != 2 {
		t.Fatalf("完整绑定应通过: %v %+v", err, got)
	}
	for name, bad := range map[string][]matching.EpisodeBinding{
		"missing":   {{FileName: "a - 12.mkv", StoredNumber: 1}},
		"duplicate": {{FileName: "a - 12.mkv", StoredNumber: 1}, {FileName: "a - 12.mkv", StoredNumber: 3}, {FileName: "a - 13.mkv", StoredNumber: 2}},
		"zero":      {{FileName: "a - 12.mkv", StoredNumber: 1}, {FileName: "a - 13.mkv", StoredNumber: 0}},
	} {
		if _, err := presetTrustedBindings(anime, files, bad, 0); !errors.Is(err, ErrMatchReviewRequired) {
			t.Fatalf("%s: 应 review_required，got %v", name, err)
		}
	}
	// 偏移适用性与 trustedTargetBindings 同口径：集号 <= 话数时不适用。
	if _, err := presetTrustedBindings(anime, []string{"a - 05.mkv"}, []matching.EpisodeBinding{{FileName: "a - 05.mkv", StoredNumber: 1}}, 4); !errors.Is(err, ErrMatchReviewRequired) {
		t.Fatalf("偏移不适用应 review_required，got %v", err)
	}
}

func animeForPreset() models.Anime {
	return models.Anime{ID: 1, BangumiID: 1, EpCount: 8, FilePath: "a"}
}

// PR #20 复审问题 2（仙王的日常生活 第五季 回放）：第 3 集与 4.5 集同批预览时，4.5 无有效绑定，
// 预览必须与受信任提交同口径整体复核，而不是只给出第 3 集绑定并报 matched。
func TestAssistHintPreviewRequiresBindingForEveryFileXianWangReplay(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	dir := filepath.Join("仙王", "S5")
	names := []string{
		"[GM-Team][国漫][仙王的日常生活 第5季][Xian Wang De Ri Chang Sheng Huo Ⅴ][2025][03][AVC][GB][1080P].mp4",
		"[GM-Team][国漫][仙王的日常生活 第5季][Xian Wang De Ri Chang Sheng Huo Ⅴ][2025][04.5][AVC][GB][1080P].mp4",
	}
	writeScanVideos(t, filepath.Join(root, dir), names...)
	fake := newFixtureProvider(t, loadProviderReplayDoc(t))
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })
	prev := scanSessionHook
	scanSessionHook = func(ctx context.Context, r *MatchingRuntime) *MatchingSession {
		return newMatchingSessionForTest(ctx, r, fake)
	}
	t.Cleanup(func() { scanSessionHook = prev })
	sc := newV2Scanner(t, root)

	for _, hint := range []string{"", "仙王的日常生活 第五季"} {
		res, err := sc.ResolveAssistByHint(context.Background(), AssistHintInput{RelDir: filepath.ToSlash(dir), TitleHint: hint, FileNames: names})
		if err != nil {
			t.Fatalf("hint=%q: %v", hint, err)
		}
		if res.Decision.Code != matching.Review || res.Subject != nil || res.Candidate != nil || len(res.Chapters.Bindings) != 0 {
			t.Fatalf("hint=%q: 部分文件无绑定时预览应整体复核，got code=%v bindings=%+v", hint, res.Decision.Code, res.Chapters.Bindings)
		}
		if !strings.Contains(res.Decision.Reason, names[1]) {
			t.Fatalf("hint=%q: 复核原因应列出 4.5 集文件，got %q", hint, res.Decision.Reason)
		}
		if len(res.Candidates) != 1 || res.Candidates[0].ID != 529875 {
			t.Fatalf("hint=%q: 应给出第五季候选供人工确认，got %+v", hint, res.Candidates)
		}
	}
	// 只含第 3 集时预览仍正常 matched。
	res, err := sc.ResolveAssistByHint(context.Background(), AssistHintInput{RelDir: filepath.ToSlash(dir), FileNames: names[:1]})
	if err != nil || res.Decision.Code != matching.Accepted || len(res.Chapters.Bindings) != 1 || res.Chapters.Bindings[0].StoredNumber != 3 {
		t.Fatalf("单文件预览应 matched ep3: %v %+v", err, res.Decision)
	}
}
