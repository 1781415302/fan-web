package services

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"fan-web/database"
	"fan-web/models"
	"fan-web/services/matching"
)

func setupReviewDB(t *testing.T) {
	t.Helper()
	if err := database.Init(filepath.Join(t.TempDir(), "review-hardening.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
			database.DB = nil
		}
	})
}

func seriesChaptersID(subjectID, n int) []matching.Chapter {
	chs := make([]matching.Chapter, 0, n)
	for i := 1; i <= n; i++ {
		chs = append(chs, matching.Chapter{ID: subjectID*1000 + i, Ep: float64(i), Sort: float64(i)})
	}
	return chs
}

// ---- P1: 跨作品误改名 ----

func TestScanForAnimeCrossWorkRenameBlockedKeepsProgress(t *testing.T) {
	setupReviewDB(t)
	root := t.TempDir()
	relDir := "alpha-dir"
	oldName := "Alpha Animation - 01.mkv"
	newName := "Beta Animation - 01.mkv"
	// 磁盘只剩 Beta；Alpha 文件已消失
	writeScanVideos(t, filepath.Join(root, relDir), newName)

	alphaSub := matching.Subject{
		ID: 51001, Name: "Alpha Animation", NameCn: "Alpha Animation",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	betaSub := matching.Subject{
		ID: 51002, Name: "Beta Animation", NameCn: "Beta Animation",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	f := newFakeSessionProvider()
	f.putSubject(alphaSub, "Alpha Animation")
	f.putSubject(betaSub, "Beta Animation")
	f.chapters[51001] = seriesChaptersID(51001, 12)
	f.chapters[51002] = seriesChaptersID(51002, 12)
	withScanSession(t, f)
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

	if _, err := database.DB.Exec(
		`INSERT INTO users (username, password, is_admin) VALUES (?, ?, ?)`, "p1-user", "x", 0,
	); err != nil {
		t.Fatal(err)
	}
	user, err := database.GetUserByUsername("p1-user")
	if err != nil {
		t.Fatal(err)
	}

	anime, err := database.CreateAnime(&models.Anime{
		Title: "Alpha Animation", TitleCn: "Alpha Animation",
		BangumiID: 51001, EpCount: 12, FilePath: relDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CreateEpisode(&models.Episode{
		AnimeID: anime.ID, EpNumber: 1, Title: "keep-me", FilePath: oldName, Duration: 1400,
	}); err != nil {
		t.Fatal(err)
	}
	eps, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil || len(eps) != 1 {
		t.Fatalf("seed eps: %v %#v", err, eps)
	}
	epID := eps[0].ID
	if err := database.UpsertProgress(user.ID, epID, 73, true); err != nil {
		t.Fatal(err)
	}
	if err := database.EnqueueBangumiOutbox(user.ID, epID); err != nil {
		t.Fatal(err)
	}

	sc := newV2Scanner(t, root)
	existing, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, scanErr := sc.ScanForAnime(*anime, existing, nil)
	if scanErr == nil || !errors.Is(scanErr, ErrMatchReviewRequired) {
		t.Fatalf("跨作品同号替换应 Review/零写入, err=%v", scanErr)
	}

	// 确认未调用方不应 Reconcile；此处显式验证库状态未变
	after, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].ID != epID || after[0].FilePath != oldName {
		t.Fatalf("episode 不得改指 Beta: %#v", after)
	}
	progress, err := database.GetProgress(user.ID, epID)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Position != 73 || !progress.Watched {
		t.Fatalf("进度应保留: %#v", progress)
	}
	outbox, err := database.ListBangumiOutbox(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(outbox) != 1 || outbox[0].EpisodeID != epID {
		t.Fatalf("outbox 应仍引用 %d: %#v", epID, outbox)
	}
}

func TestScanForAnimeSameWorkRenameStillKeepsID(t *testing.T) {
	setupReviewDB(t)
	root := t.TempDir()
	relDir := "rename-ok"
	oldName := "Show - 01 [OLD].mkv"
	newName := "Show - 01 [NEW].mkv"
	writeScanVideos(t, filepath.Join(root, relDir), newName)

	f := newFakeSessionProvider()
	sub := matching.Subject{
		ID: 9101, Name: "Show", NameCn: "Show",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	f.putSubject(sub, "Show")
	f.chapters[9101] = seriesChaptersID(9101, 12)
	withScanSession(t, f)
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

	if _, err := database.DB.Exec(
		`INSERT INTO users (username, password, is_admin) VALUES (?, ?, ?)`, "p1-rename", "x", 0,
	); err != nil {
		t.Fatal(err)
	}
	user, err := database.GetUserByUsername("p1-rename")
	if err != nil {
		t.Fatal(err)
	}
	anime, err := database.CreateAnime(&models.Anime{
		Title: "Show", BangumiID: 9101, EpCount: 12, FilePath: relDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CreateEpisode(&models.Episode{
		AnimeID: anime.ID, EpNumber: 1, Title: "keep", FilePath: oldName, Duration: 1400,
	}); err != nil {
		t.Fatal(err)
	}
	eps, _ := database.ListEpisodesByAnimeID(anime.ID)
	epID := eps[0].ID
	if err := database.UpsertProgress(user.ID, epID, 73, false); err != nil {
		t.Fatal(err)
	}

	sc := newV2Scanner(t, root)
	inv, err := sc.ScanForAnime(*anime, eps, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Renames) != 1 || inv.Renames[0].EpisodeID != epID || inv.Renames[0].To != newName {
		t.Fatalf("合法同作品改名应保留 ID: %#v", inv.Renames)
	}
	if err := database.ReconcileEpisodes(anime.ID, database.EpisodeReconcileInput{
		ExpectedAnime: *anime,
		Expected:      eps,
		Desired:       inv.Desired,
		PresentFiles:  inv.PresentFiles,
		Renames:       inv.Renames,
		RemoveMissing: true,
	}); err != nil {
		t.Fatal(err)
	}
	after, _ := database.ListEpisodesByAnimeID(anime.ID)
	if len(after) != 1 || after[0].ID != epID || after[0].FilePath != newName {
		t.Fatalf("reconcile 后应保留 ID 并更新路径: %#v", after)
	}
	progress, _ := database.GetProgress(user.ID, epID)
	if progress.Position != 73 {
		t.Fatalf("进度应保留: %#v", progress)
	}
}

// ---- P2a: 旧绑定冲突后继续检索 ----

func starRiverFixtures(f *fakeSessionProvider) {
	f.exactOnly = true
	s1 := matching.Subject{
		ID: 62001, Name: "星河物语", NameCn: "星河物语",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	s2 := matching.Subject{
		ID: 62002, Name: "星河物语 第二季", NameCn: "星河物语 第二季",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	f.putSubject(s1, "星河物语")
	f.putSubject(s2, "星河物语 第二季", "星河物语 第2季")
	// 裸「星河物语」查询在第二季场景也会发出；返回 S1+S2 以便评分择季
	f.search["星河物语"] = matchingSearchResult{
		Subjects: []matching.Subject{
			func() matching.Subject { s := cloneSubject(s1); s.Detailed = false; return s }(),
			func() matching.Subject { s := cloneSubject(s2); s.Detailed = false; return s }(),
		},
		Source: "v0",
	}
	f.chapters[62001] = seriesChaptersID(62001, 12)
	f.chapters[62002] = seriesChaptersID(62002, 12)
}

func TestMatchingSessionBoundSeasonConflictContinuesSearch(t *testing.T) {
	rt := testSessionRuntime(t)
	f := newFakeSessionProvider()
	starRiverFixtures(f)

	file := "星河物语 第二季 - 01.mkv"
	g := groupFromFile(t, file, "xinghe")

	// 对照：无 bound 应接受第二季
	sessA := newMatchingSessionForTest(context.Background(), rt, f)
	resA, err := sessA.Resolve(g, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resA.Decision.Code != matching.Accepted || resA.Decision.WinnerID != 62002 {
		t.Fatalf("无 bound 对照应接受 S2: code=%s id=%d reason=%s cands=%v",
			resA.Decision.Code, resA.Decision.WinnerID, resA.Decision.Reason, summarizeCands(resA.Decision.Candidates))
	}

	// 有第一季旧绑定：不得因快通道直接 Review；应继续检索并接受 S2
	f.searchCalls = nil
	sessB := newMatchingSessionForTest(context.Background(), rt, f)
	bound := &BoundMatch{
		Subject: matching.Subject{
			ID: 62001, Name: "星河物语", NameCn: "星河物语",
			Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
		},
		UniqueDirectory: true,
	}
	resB, err := sessB.Resolve(g, bound)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.searchCalls) == 0 {
		t.Fatal("旧绑定季冲突后应继续搜索，searchCalls 不得为 0")
	}
	if resB.Decision.Code != matching.Accepted || resB.Decision.WinnerID != 62002 {
		t.Fatalf("有 S1 bound 仍应接受 S2: code=%s id=%d reason=%s cands=%v searches=%v",
			resB.Decision.Code, resB.Decision.WinnerID, resB.Decision.Reason, summarizeCands(resB.Decision.Candidates), f.searchCalls)
	}
	if resB.FromExistingBound {
		t.Fatal("不得 FromExistingBound 复用第一季")
	}
}

func TestMatchingSessionBoundOVAStillNotAcceptedAsTV(t *testing.T) {
	rt := testSessionRuntime(t)
	f := newFakeSessionProvider()
	f.exactOnly = true
	tv := matching.Subject{
		ID: 63001, Name: "示例作品", NameCn: "示例作品",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	ova := matching.Subject{
		ID: 63002, Name: "示例作品 OVA", NameCn: "示例作品 OVA",
		Media: matching.KindOVA, EpisodeCount: 1, Detailed: true,
	}
	f.putSubject(ova, "示例作品", "示例作品 OVA")
	f.detail[63001] = tv
	f.chapters[63002] = []matching.Chapter{{ID: 1, Ep: 1, Sort: 1}}

	sess := newMatchingSessionForTest(context.Background(), rt, f)
	g := groupFromFile(t, "示例作品 OVA - 01.mkv", "show")
	g.Query.Media = matching.KindOVA
	g.Query.Titles = []matching.Title{{Text: "示例作品", Source: matching.SourceFile}}
	g.Query.HasFileTitle = true
	if len(g.Files) > 0 {
		g.Files[0].Media = matching.KindOVA
		g.Files[0].Titles = g.Query.Titles
	}
	bound := &BoundMatch{Subject: tv, UniqueDirectory: true}
	res, err := sess.Resolve(g, bound)
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision.Code == matching.Accepted && res.Decision.WinnerID == tv.ID {
		t.Fatalf("OVA 不得被接受为 TV 绑定: %+v", res.Decision)
	}
	if res.FromExistingBound {
		t.Fatal("FromExistingBound must be false")
	}
	// 有正确 OVA 候选时应接受 OVA，而非停在 Review
	if res.Decision.Code == matching.Accepted && res.Decision.WinnerID != ova.ID {
		t.Fatalf("want OVA %d, got %d", ova.ID, res.Decision.WinnerID)
	}
}

func TestLibraryScanBoundSeasonConflictFindsCorrectSeason(t *testing.T) {
	setupReviewDB(t)
	root := t.TempDir()
	relDir := "xinghe-lib"
	fname := "星河物语 第二季 - 01.mkv"
	writeScanVideos(t, filepath.Join(root, relDir), fname)

	// 目录已绑定第一季
	s1Anime, err := database.CreateAnime(&models.Anime{
		Title: "星河物语", TitleCn: "星河物语", BangumiID: 62001, EpCount: 12, FilePath: relDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	f := newFakeSessionProvider()
	starRiverFixtures(f)
	svc := newLibraryV2Service(t, root, f)
	res, err := svc.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if res.NewAnimes < 1 && res.NewEpisodes < 1 {
		// 可能 NewAnimes=1（第二季）或挂到已有——以 Bangumi 行为准
		t.Logf("scan result: %#v unidentified=%d", res, len(res.Unidentified))
	}

	s2Anime, err := database.GetAnimeByBangumiID(62002)
	if err != nil {
		t.Fatal(err)
	}
	if s2Anime == nil {
		t.Fatalf("应创建/关联第二季；unidentified=%v result=%#v", res.Unidentified, res)
	}
	eps2, err := database.ListEpisodesByAnimeID(s2Anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps2) != 1 || eps2[0].FilePath != fname || eps2[0].EpNumber != 1 {
		t.Fatalf("第二季应绑定该文件: %#v", eps2)
	}

	// 第一季数据不变、无进度被偷
	s1After, err := database.GetAnimeByID(s1Anime.ID)
	if err != nil || s1After == nil || s1After.BangumiID != 62001 {
		t.Fatalf("第一季 anime 应不变: %#v err=%v", s1After, err)
	}
	eps1, _ := database.ListEpisodesByAnimeID(s1Anime.ID)
	if len(eps1) != 0 {
		t.Fatalf("第一季不应吞入第二季文件: %#v", eps1)
	}
}

// ---- P2b: 候选池跨组隔离 ----

func mirrorTaleFixtures(f *fakeSessionProvider) {
	f.exactOnly = true
	s1 := matching.Subject{
		ID: 71001, Name: "镜像物语", NameCn: "镜像物语",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
		Aliases: []string{"Mirror Tale"},
	}
	s2 := matching.Subject{
		ID: 71002, Name: "镜像物语 第二季", NameCn: "镜像物语 第二季",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
		Aliases: []string{"Mirror Tale"},
	}
	f.detail[s1.ID] = cloneSubject(s1)
	f.detail[s2.ID] = cloneSubject(s2)
	stub := func(sub matching.Subject) matching.Subject {
		s := cloneSubject(sub)
		s.Detailed = false
		return s
	}
	// 中文第二季相关 → S2；英文 Mirror Tale → S1；裸中文 base → S1
	f.search["镜像物语 第二季"] = matchingSearchResult{Subjects: []matching.Subject{stub(s2)}, Source: "v0"}
	f.search["镜像物语 第2季"] = matchingSearchResult{Subjects: []matching.Subject{stub(s2)}, Source: "v0"}
	f.search["镜像物语"] = matchingSearchResult{Subjects: []matching.Subject{stub(s1)}, Source: "v0"}
	f.search["Mirror Tale"] = matchingSearchResult{Subjects: []matching.Subject{stub(s1)}, Source: "v0"}
	f.chapters[71001] = seriesChaptersID(71001, 12)
	f.chapters[71002] = seriesChaptersID(71002, 12)
}

func TestMatchingSessionPoolIsolatedAcrossGroups(t *testing.T) {
	rt := testSessionRuntime(t)
	f := newFakeSessionProvider()
	mirrorTaleFixtures(f)

	gEN := groupFromFile(t, "Mirror Tale - 01.mkv", "mirror")
	gCN2 := groupFromFile(t, "镜像物语 第二季 - 01.mkv", "mirror")

	// A: 新 Session 只匹配英文 → S1
	sessA := newMatchingSessionForTest(context.Background(), rt, f)
	resA, err := sessA.Resolve(gEN, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resA.Decision.Code != matching.Accepted || resA.Decision.WinnerID != 71001 {
		t.Fatalf("A 应接受 S1: code=%s id=%d cands=%v", resA.Decision.Code, resA.Decision.WinnerID, summarizeCands(resA.Decision.Candidates))
	}

	// B: 同 Session 先中文第二季再英文 → 英文仍须 S1（不得因 pool 残留 S2 变 Review）
	sessB := newMatchingSessionForTest(context.Background(), rt, f)
	resB1, err := sessB.Resolve(gCN2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resB1.Decision.Code != matching.Accepted || resB1.Decision.WinnerID != 71002 {
		t.Fatalf("B1 应接受 S2: code=%s id=%d cands=%v", resB1.Decision.Code, resB1.Decision.WinnerID, summarizeCands(resB1.Decision.Candidates))
	}
	resB2, err := sessB.Resolve(gEN, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resB2.Decision.Code != matching.Accepted || resB2.Decision.WinnerID != 71001 {
		t.Fatalf("B2 应仍接受 S1（候选池隔离）: code=%s id=%d reason=%s cands=%v",
			resB2.Decision.Code, resB2.Decision.WinnerID, resB2.Decision.Reason, summarizeCands(resB2.Decision.Candidates))
	}

	// 反向：先英文再中文第二季
	sessC := newMatchingSessionForTest(context.Background(), rt, f)
	resC1, _ := sessC.Resolve(gEN, nil)
	resC2, err := sessC.Resolve(gCN2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resC1.Decision.WinnerID != 71001 || resC2.Decision.Code != matching.Accepted || resC2.Decision.WinnerID != 71002 {
		t.Fatalf("反向顺序失败: C1=%d C2 code=%s id=%d", resC1.Decision.WinnerID, resC2.Decision.Code, resC2.Decision.WinnerID)
	}

	// 重复运行：决定与 winner 一致
	sessD := newMatchingSessionForTest(context.Background(), rt, f)
	_, _ = sessD.Resolve(gCN2, nil)
	r1, _ := sessD.Resolve(gEN, nil)
	r2, _ := sessD.Resolve(gEN, nil)
	if r1.Decision.WinnerID != r2.Decision.WinnerID || r1.Decision.Code != r2.Decision.Code {
		t.Fatalf("重复运行不一致: %#v vs %#v", r1.Decision, r2.Decision)
	}

	// 缓存命中：同一 fake 上第二次 Session 仍隔离
	sessE := newMatchingSessionForTest(context.Background(), rt, f)
	_, _ = sessE.Resolve(gCN2, nil)
	resE, err := sessE.Resolve(gEN, nil)
	if err != nil || resE.Decision.WinnerID != 71001 {
		t.Fatalf("缓存命中后仍应 S1: err=%v dec=%+v", err, resE.Decision)
	}
}

func TestMatchingSessionPoolCacheHitStillMergesQueryResults(t *testing.T) {
	rt := testSessionRuntime(t)
	f := newFakeSessionProvider()
	mirrorTaleFixtures(f)

	// 预热 detail/search「缓存」（fake map）
	_, _ = f.Search(context.Background(), "Mirror Tale")
	_, _ = f.Detail(context.Background(), 71001)

	sess := newMatchingSessionForTest(context.Background(), rt, f)
	// 先污染：解析第二季写入 session pool 缓存
	_, err := sess.Resolve(groupFromFile(t, "镜像物语 第二季 - 01.mkv", "m"), nil)
	if err != nil {
		t.Fatal(err)
	}
	beforeCalls := len(f.searchCalls)
	res, err := sess.Resolve(groupFromFile(t, "Mirror Tale - 01.mkv", "m"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.searchCalls) <= beforeCalls {
		t.Fatal("命中 provider 层缓存时仍应执行 Search 并把该查询返回合并进本组候选")
	}
	if res.Decision.Code != matching.Accepted || res.Decision.WinnerID != 71001 {
		t.Fatalf("want S1, got code=%s id=%d", res.Decision.Code, res.Decision.WinnerID)
	}
}

// ---- 并列语言片段歧义不得被近似第三作绕过 ----

func starAmbiguityFixtures(f *fakeSessionProvider) {
	f.exactOnly = true
	a := matching.Subject{
		ID: 830001, Name: "关于我们在星河深处共同旅行并寻找故乡的漫长故事",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	b := matching.Subject{
		ID: 830002, Name: "The Unexpected Chronicle of a Journey Across the Stars",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	c := matching.Subject{
		ID: 830003, Name: "关于我们在星河远处共同旅行并寻找故乡的漫长故事",
		Aliases: []string{"The Unexpected Chronicla of a Journey Across the Stars"},
		Media:   matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	cn := "关于我们在星河深处共同旅行并寻找故乡的漫长故事"
	en := "The Unexpected Chronicle of a Journey Across the Stars"
	// 合法查询显式注册固定 A/B/C；禁止子串兜底
	f.putSubject(a, cn, en)
	f.putSubject(b, cn, en)
	f.putSubject(c, cn, en)
	f.chapters[830001] = []matching.Chapter{{ID: 83000101, Ep: 1, Sort: 1}}
	f.chapters[830002] = []matching.Chapter{{ID: 83000201, Ep: 1, Sort: 1}}
	f.chapters[830003] = []matching.Chapter{{ID: 83000301, Ep: 1, Sort: 1}}
}

func TestMatchingSessionFragmentConflictNotBypassedByApproxThird(t *testing.T) {
	rt := testSessionRuntime(t)
	file := "[Sub][关于我们在星河深处共同旅行并寻找故乡的漫长故事][The Unexpected Chronicle of a Journey Across the Stars][01].mkv"
	g := groupFromFile(t, file, "ambig")

	run := func(t *testing.T) matching.Decision {
		t.Helper()
		f := newFakeSessionProvider()
		starAmbiguityFixtures(f)
		sess := newMatchingSessionForTest(context.Background(), rt, f)
		res, err := sess.Resolve(g, nil)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		return res.Decision
	}

	d := run(t)
	if d.Code != matching.Review || d.WinnerID != 0 {
		t.Fatalf("want Review/0, got code=%s winner=%d reason=%s cands=%v",
			d.Code, d.WinnerID, d.Reason, summarizeCands(d.Candidates))
	}
	for _, id := range []int{830001, 830002, 830003} {
		if d.Code == matching.Accepted && d.WinnerID == id {
			t.Fatalf("must not auto-link any of A/B/C, got %d", id)
		}
	}

	// 调换 put 顺序（ID 排列）仍拒绝：重建 fixture 时改 subject 写入顺序
	f2 := newFakeSessionProvider()
	f2.exactOnly = true
	subs := []matching.Subject{
		{ID: 830003, Name: "关于我们在星河远处共同旅行并寻找故乡的漫长故事", Aliases: []string{"The Unexpected Chronicla of a Journey Across the Stars"}, Media: matching.KindSeries, EpisodeCount: 12, Detailed: true},
		{ID: 830002, Name: "The Unexpected Chronicle of a Journey Across the Stars", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true},
		{ID: 830001, Name: "关于我们在星河深处共同旅行并寻找故乡的漫长故事", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true},
	}
	cn := "关于我们在星河深处共同旅行并寻找故乡的漫长故事"
	en := "The Unexpected Chronicle of a Journey Across the Stars"
	for _, s := range subs {
		f2.putSubject(s, cn, en)
		f2.chapters[s.ID] = []matching.Chapter{{ID: s.ID*100 + 1, Ep: 1, Sort: 1}}
	}
	sess2 := newMatchingSessionForTest(context.Background(), rt, f2)
	res2, err := sess2.Resolve(g, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Decision.Code != matching.Review || res2.Decision.WinnerID != 0 {
		t.Fatalf("reordered IDs still Review: code=%s winner=%d reason=%s",
			res2.Decision.Code, res2.Decision.WinnerID, res2.Decision.Reason)
	}
}

func TestMatchingSessionBlockersStillExcludedFromGap(t *testing.T) {
	// 明确 Blockers 的竞争者仍可合理排除，不因本次收紧而卡死可接受胜者。
	rt := testSessionRuntime(t)
	f := newFakeSessionProvider()
	f.exactOnly = true
	tv := matching.Subject{
		ID: 840001, Name: "深空航线", NameCn: "深空航线",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	movie := matching.Subject{
		ID: 840002, Name: "深空航线", NameCn: "深空航线",
		Media: matching.KindMovie, EpisodeCount: 1, Detailed: true,
	}
	f.putSubject(tv, "深空航线")
	f.putSubject(movie, "深空航线")
	f.chapters[840001] = seriesChaptersID(840001, 12)
	f.chapters[840002] = []matching.Chapter{{ID: 84000201, Ep: 1, Sort: 1}}

	sess := newMatchingSessionForTest(context.Background(), rt, f)
	g := groupFromFile(t, "深空航线 - 01.mkv", "deep")
	g.Query.Media = matching.KindSeries
	if len(g.Files) > 0 {
		g.Files[0].Media = matching.KindSeries
	}
	res, err := sess.Resolve(g, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision.Code != matching.Accepted || res.Decision.WinnerID != tv.ID {
		t.Fatalf("series should accept despite movie blocker peer: code=%s id=%d reason=%s cands=%v",
			res.Decision.Code, res.Decision.WinnerID, res.Decision.Reason, summarizeCands(res.Decision.Candidates))
	}
}
