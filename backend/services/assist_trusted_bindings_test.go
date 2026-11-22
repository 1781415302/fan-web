package services

import (
	"context"
	"errors"
	"path/filepath"
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
