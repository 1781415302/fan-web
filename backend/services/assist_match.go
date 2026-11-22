package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"fan-web/database"
	"fan-web/models"
	"fan-web/services/matching"
)

// ErrAssistInvalidInput 辅助匹配入参非法（路径指向非视频/symlink、file_names 不合法或不在目录中等）。
var ErrAssistInvalidInput = errors.New("辅助匹配参数无效")

// assistMaxCandidates 未自动接受时最多返回的候选数。
const assistMaxCandidates = 3

// AssistInputResolved 辅助匹配入参路径解析结果（目录或单文件统一为相对目录 + 文件名）。
type AssistInputResolved struct {
	RelDir           string
	PresentFiles     []string // 目录现存全量合法视频名（单文件模式为 1 个）
	FreeFiles        []string // 待处理的未关联文件名（或显式指定的 file_names）
	IsSingleFilePath bool
}

// AssistHintInput ResolveAssistByHint 的输入。
type AssistHintInput struct {
	RelDir, TitleHint         string
	FileNames                 []string
	SeasonHint, EpisodeOffset int
}

// AssistHintResult 辅助匹配解析结果（零 DB 写）。
type AssistHintResult struct {
	Decision   matching.Decision
	Subject    *matching.Subject
	Chapters   matching.ChapterDecision
	Candidate  *models.MatchCandidate
	Bindings   []models.AssistMatchBinding
	Candidates []models.MatchCandidate
}

// AssistOwnershipStatus 目标文件在 relDir 下的归属情况。
type AssistOwnershipStatus struct {
	OwnedAnime     *models.Anime    // 全归同一番剧或部分已归同一番剧时非 nil
	OwnedEpisodes  []models.Episode // 对应已入库剧集
	Unassociated   []string         // 待处理中尚未入库的 file_names
	ConflictFiles  []string         // 归属不同/冲突番剧的文件名
	ConflictReason string
}

func assistInvalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrAssistInvalidInput, fmt.Sprintf(format, args...))
}

// validateFileNameStrict 显式 file_names 的严格校验：拒空/全空白、含 / \ .. 或空字节。
func validateFileNameStrict(name string) error {
	if strings.TrimSpace(name) == "" {
		return assistInvalid("文件名不能为空")
	}
	if strings.ContainsRune(name, '\x00') || strings.Contains(name, "/") ||
		strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return assistInvalid("文件名不合法: %q", name)
	}
	return nil
}

// ResolveAssistInputPath 把 file_path（目录或单个视频文件）与可选 file_names 解析为相对目录与待处理文件。
func (s *ScannerService) ResolveAssistInputPath(rawPath string, fileNames []string) (AssistInputResolved, error) {
	var out AssistInputResolved
	if s == nil {
		return out, fmt.Errorf("ScannerService 为空")
	}
	if err := ValidateRelativeVideoPath(rawPath); err != nil {
		return out, err
	}
	normPath := NormalizeRelDir(rawPath)
	root, err := s.resolveDirectory(".")
	if err != nil {
		return out, err
	}
	cand := filepath.Join(root, filepath.FromSlash(normPath))
	info, lerr := os.Lstat(cand)
	if lerr == nil && !info.IsDir() {
		if info.Mode()&os.ModeSymlink != 0 {
			return out, assistInvalid("不允许使用符号链接文件: %s", normPath)
		}
		if strings.Contains(cand, ":Zone.Identifier") {
			return out, assistInvalid("不支持的文件: %s", normPath)
		}
		if !videoExts[strings.ToLower(filepath.Ext(cand))] || !info.Mode().IsRegular() {
			return out, assistInvalid("不是受支持的视频文件: %s", normPath)
		}
		resolved, err := filepath.EvalSymlinks(cand)
		if err != nil {
			return out, fmt.Errorf("解析视频文件失败: %w", err)
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return out, ErrInvalidVideoPath
		}
		base := path.Base(normPath)
		if len(fileNames) > 0 && (len(fileNames) != 1 || fileNames[0] != filepath.Base(normPath)) {
			return out, assistInvalid("file_path 指向单个文件时 file_names 必须为空或与该文件一致")
		}
		out.RelDir = NormalizeRelDir(filepath.ToSlash(path.Dir(normPath)))
		out.PresentFiles = []string{base}
		out.FreeFiles = []string{base}
		out.IsSingleFilePath = true
		// 单文件即使已关联也照常交给下游 InspectAssistFilesOwnership 判定幂等/冲突。
		return out, nil
	}

	_, present, err := s.listPresentVideoFiles(normPath)
	if err != nil {
		return out, err
	}
	out.RelDir = normPath
	out.PresentFiles = append([]string{}, present...)
	out.FreeFiles = []string{}
	if len(fileNames) > 0 {
		presentSet := make(map[string]bool, len(present))
		for _, n := range present {
			presentSet[n] = true
		}
		seen := make(map[string]bool, len(fileNames))
		for _, n := range fileNames {
			if err := validateFileNameStrict(n); err != nil {
				return out, err
			}
			if !presentSet[n] {
				return out, assistInvalid("目录中不存在该视频文件: %s", n)
			}
			if seen[n] {
				continue
			}
			seen[n] = true
			out.FreeFiles = append(out.FreeFiles, n)
		}
		return out, nil
	}
	for _, n := range present {
		associated, err := fileIsAssociated(n, normPath)
		if err != nil {
			return out, fmt.Errorf("检查文件关联失败: %w", err)
		}
		if associated {
			continue
		}
		out.FreeFiles = append(out.FreeFiles, n)
	}
	return out, nil
}

// InspectAssistFilesOwnership 统计 relDir 下目标文件被 0/1/多部番剧认领的情况（只读）。
func (s *ScannerService) InspectAssistFilesOwnership(relDir string, fileNames []string) (AssistOwnershipStatus, error) {
	out := AssistOwnershipStatus{
		OwnedEpisodes: []models.Episode{},
		Unassociated:  []string{},
		ConflictFiles: []string{},
	}
	animes, err := database.ListAnimesByFilePath(relDir)
	if err != nil {
		return out, fmt.Errorf("查询目录番剧失败: %w", err)
	}
	type owner struct {
		anime   int // animes 下标
		episode models.Episode
	}
	owners := map[string][]owner{}
	for i, a := range animes {
		eps, err := database.ListEpisodesByAnimeID(a.ID)
		if err != nil {
			return out, fmt.Errorf("查询番剧剧集失败: %w", err)
		}
		for _, ep := range eps {
			if ep.FilePath == "" {
				continue
			}
			key := strings.ReplaceAll(ep.FilePath, "\\", "/")
			owners[key] = append(owners[key], owner{anime: i, episode: ep})
		}
	}

	seen := map[string]bool{}
	ownedBy := map[int][]owner{} // 仅被唯一一部番剧认领的文件
	var ownedNames []string
	var multiOwned []string
	for _, name := range fileNames {
		key := strings.ReplaceAll(name, "\\", "/")
		if seen[key] {
			continue
		}
		seen[key] = true
		claims := owners[key]
		animeSet := map[int]bool{}
		for _, o := range claims {
			animeSet[o.anime] = true
		}
		switch len(animeSet) {
		case 0:
			out.Unassociated = append(out.Unassociated, name)
		case 1:
			idx := claims[0].anime
			ownedBy[idx] = append(ownedBy[idx], claims...)
			ownedNames = append(ownedNames, name)
		default:
			multiOwned = append(multiOwned, name)
		}
	}

	switch {
	case len(multiOwned) > 0:
		out.ConflictFiles = append(out.ConflictFiles, multiOwned...)
		if len(ownedBy) > 1 {
			out.ConflictFiles = append(out.ConflictFiles, ownedNames...)
		}
		out.ConflictReason = "文件同时被多部番剧认领"
	case len(ownedBy) > 1:
		out.ConflictFiles = append(out.ConflictFiles, ownedNames...)
		out.ConflictReason = "目标文件分属不同番剧"
	}
	if len(ownedBy) == 1 {
		for idx, list := range ownedBy {
			a := animes[idx]
			out.OwnedAnime = &a
			for _, o := range list {
				out.OwnedEpisodes = append(out.OwnedEpisodes, o.episode)
			}
		}
		sort.SliceStable(out.OwnedEpisodes, func(i, j int) bool {
			if out.OwnedEpisodes[i].EpNumber != out.OwnedEpisodes[j].EpNumber {
				return out.OwnedEpisodes[i].EpNumber < out.OwnedEpisodes[j].EpNumber
			}
			return out.OwnedEpisodes[i].ID < out.OwnedEpisodes[j].ID
		})
	}
	return out, nil
}

// PreviewAssistByBangumiID bangumi_id 直绑的零写库预览：详情 + 章节 + InterpretChapters。
// 章节冲突、任一文件被拒或有文件未绑定（含无集号非电影文件）时返回 Review；
// 条目不存在/无章节且话数未知同样按 Review 返回；上游网络错误返回 error。
func (s *ScannerService) PreviewAssistByBangumiID(ctx context.Context, relDir string, fileNames []string, bangumiID, episodeOffset int) (AssistHintResult, error) {
	out := emptyAssistHintResult()
	if s == nil || s.matching == nil || s.MatchEngine() != matching.EngineV2 {
		return out, fmt.Errorf("辅助匹配需要 v2 匹配引擎")
	}
	if bangumiID <= 0 {
		return out, assistInvalid("bangumi_id 无效")
	}
	if len(fileNames) == 0 {
		return out, assistInvalid("没有待匹配的文件")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	files := make([]matching.FileEvidence, 0, len(fileNames))
	for _, fn := range fileNames {
		files = append(files, matching.ParseFile(matching.FileInput{Name: fn, Dir: relDir}))
	}
	origFiles := files
	if episodeOffset > 0 {
		if !assistOffsetApplicable(files, episodeOffset) {
			out.Decision = reviewDecision(errAssistOffsetTooLarge)
			return out, nil
		}
		// 仅对副本减偏移（ShiftGroupEpisodes 不修改入参底层数组）
		files = matching.ShiftGroupEpisodes(matching.Group{Files: files}, episodeOffset).Files
	}
	sess := s.openMatchingSession(ctx)
	if sess == nil || sess.provider == nil {
		return out, fmt.Errorf("辅助匹配: MatchingSession/Provider 不可用")
	}
	sub, ch, err := interpretTrustedTargetChapters(sess, bangumiID, files)
	if err != nil {
		if errors.Is(err, ErrMatchReviewRequired) {
			out.Decision = reviewDecision(strings.TrimPrefix(err.Error(), ErrMatchReviewRequired.Error()+": "))
			return out, nil
		}
		return out, err
	}
	out.Chapters = ch
	subCopy := sub
	out.Subject = &subCopy
	// 与受信任提交（trustedTargetBindings）同口径：拿到详情话数后补齐累计编号适用性校验
	if reason := assistEpisodeOffsetReason(origFiles, episodeOffset, sub.EpisodeCount); reason != "" {
		out.Decision = reviewDecision(reason)
		out.Candidates = []models.MatchCandidate{modelCandidateFromSubject(sub, 1.0)}
		return out, nil
	}
	if reason := trustedChapterReviewReason(ch, files); reason != "" {
		out.Decision = reviewDecision(reason)
		out.Candidates = []models.MatchCandidate{modelCandidateFromSubject(sub, 1.0)}
		return out, nil
	}
	out.Decision = matching.Decision{Code: matching.Accepted, WinnerID: bangumiID, Candidates: []matching.Candidate{}}
	out.Candidate = assistCandidatePtr(modelCandidateFromSubject(sub, 1.0))
	out.Bindings = assistBindings(ch.Bindings)
	return out, nil
}

// errAssistOffsetTooLarge episode_offset 不小于文件集号时的复核原因。
const errAssistOffsetTooLarge = "episode_offset 不能大于或等于文件集号"

// ResolveAssistByHint 无 bangumi_id 时按 title_hint（可空）构造单组并 Resolve（零 DB 写）。
// 分支 A（无 hint）：GroupFiles([]FileInput) → ShiftGroupEpisodes → SeasonHint 覆写；
// 分支 B（有 hint）：逐个 ParseFile → 原地平移 → 合成单组 → 一次性构造 Query（不再 ShiftGroupEpisodes）。
func (s *ScannerService) ResolveAssistByHint(ctx context.Context, input AssistHintInput) (AssistHintResult, error) {
	out := emptyAssistHintResult()
	if s == nil || s.matching == nil || s.MatchEngine() != matching.EngineV2 {
		return out, fmt.Errorf("辅助匹配需要 v2 匹配引擎")
	}
	if len(input.FileNames) == 0 {
		return out, assistInvalid("没有待匹配的文件")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	group, reason := buildAssistHintGroup(input)
	if reason != "" {
		out.Decision = reviewDecision(reason)
		return out, nil
	}

	sess := s.openMatchingSession(ctx)
	if sess == nil || sess.provider == nil {
		return out, fmt.Errorf("辅助匹配: MatchingSession/Provider 不可用")
	}
	res, err := sess.Resolve(group, nil)
	if err != nil {
		return out, err
	}
	out.Decision = res.Decision
	if out.Decision.Candidates == nil {
		out.Decision.Candidates = []matching.Candidate{}
	}
	out.Subject = res.Subject
	out.Chapters = res.Chapters
	if res.Decision.Code == matching.Accepted && res.Subject != nil && input.EpisodeOffset > 0 {
		// 与受信任提交同口径：胜者详情话数已知后补齐累计编号适用性校验（group 已平移，还原原始集号）
		if reason := assistEpisodeOffsetReason(unshiftAssistFiles(group.Files, input.EpisodeOffset), input.EpisodeOffset, res.Subject.EpisodeCount); reason != "" {
			out.Decision = reviewDecision(reason)
			out.Subject = nil
			out.Chapters = emptyAssistHintResult().Chapters
			out.Candidates = []models.MatchCandidate{modelCandidateFromSubject(*res.Subject, 1.0)}
			return out, nil
		}
	}
	if res.Decision.Code == matching.Accepted && res.Subject != nil {
		score := 1.0
		for _, c := range res.Decision.Candidates {
			if c.Subject.ID == res.Decision.WinnerID {
				score = c.Score
				break
			}
		}
		// 与受信任提交（presetTrustedBindings）同口径：整批每个文件都必须有有效绑定，否则整体复核，
		// 避免部分文件（如 4.5 这类无法解释的集号）未绑定时预览仍报 matched。
		if _, missing := bindingsForEveryFile(input.FileNames, res.Chapters.Bindings); len(missing) > 0 {
			out.Decision = reviewDecision(missingBindingsReason(missing))
			out.Subject = nil
			out.Chapters = emptyAssistHintResult().Chapters
			out.Candidates = []models.MatchCandidate{modelCandidateFromSubject(*res.Subject, score)}
			return out, nil
		}
		out.Candidate = assistCandidatePtr(modelCandidateFromSubject(*res.Subject, score))
		out.Bindings = assistBindings(res.Chapters.Bindings)
		return out, nil
	}
	for _, c := range res.Decision.Candidates {
		if len(out.Candidates) >= assistMaxCandidates {
			break
		}
		if c.Subject.ID <= 0 {
			continue
		}
		out.Candidates = append(out.Candidates, modelCandidateFromSubject(c.Subject, c.Score))
	}
	return out, nil
}

// buildAssistHintGroup 按 §4.5 双分支构造待 Resolve 的单组；第二个返回值非空表示直接 Review 的原因。
// 分支 A（TitleHint 为空）：GroupFiles([]FileInput) → ShiftGroupEpisodes → SeasonHint 覆写并剔除季号冲突。
// 分支 B（TitleHint 非空）：不调用 GroupFiles；逐个 ParseFile → 原地平移集号 → 合成单组 →
// 一次性构造 Query（之后绝不再调用 ShiftGroupEpisodes，避免 hint 被重建 Query 覆盖）。
func buildAssistHintGroup(input AssistHintInput) (matching.Group, string) {
	var group matching.Group
	if hint := strings.TrimSpace(input.TitleHint); hint == "" {
		// —— 分支 A：无提示词回退路径 ——
		inputs := make([]matching.FileInput, 0, len(input.FileNames))
		for _, fn := range input.FileNames {
			inputs = append(inputs, matching.FileInput{Name: fn, Dir: input.RelDir})
		}
		groups := matching.GroupFiles(inputs)
		if len(groups) != 1 {
			return matching.Group{}, "目标文件属于多个不同标题分组"
		}
		group = groups[0]
		if input.EpisodeOffset > 0 {
			if !assistOffsetApplicable(group.Files, input.EpisodeOffset) {
				return matching.Group{}, errAssistOffsetTooLarge
			}
			group = matching.ShiftGroupEpisodes(group, input.EpisodeOffset)
		}
		if input.SeasonHint > 0 {
			group.Query.Ordinal.Season = input.SeasonHint
			group.Query.Conflicts = dropSeasonConflicts(group.Query.Conflicts)
		}
	} else {
		// —— 分支 B：有外部提示词路径，不调用 GroupFiles ——
		files := make([]matching.FileEvidence, 0, len(input.FileNames))
		for _, fn := range input.FileNames {
			files = append(files, matching.ParseFile(matching.FileInput{Name: fn, Dir: input.RelDir}))
		}
		if input.EpisodeOffset > 0 {
			if !assistOffsetApplicable(files, input.EpisodeOffset) {
				return matching.Group{}, errAssistOffsetTooLarge
			}
			for i := range files {
				if files[i].Episode.Kind != matching.NumberSingle {
					continue
				}
				files[i].Episode.First -= input.EpisodeOffset
				files[i].Episode.Last -= input.EpisodeOffset
				files[i].AbsEpisode = 0
			}
		}
		group = matching.Group{Key: "assist:" + input.RelDir, Dir: input.RelDir, Files: files}
		group.Query = buildAssistHintQuery(hint, files, input.SeasonHint)
	}
	return group, ""
}

// buildAssistHintQuery 分支 B 一次性构造组级 Query：标题取 hint；季号优先级
// SeasonHint > hint 季号 > 文件一致季号；Part/Final/Year/Media 优先 hint，缺失时取文件一致值。
func buildAssistHintQuery(hint string, files []matching.FileEvidence, seasonHint int) matching.Query {
	hintFacts := matching.AnalyzeTitle(hint)
	q := matching.Query{
		Titles:       []matching.Title{{Text: hint, Source: matching.SourceFile}},
		HasFileTitle: true,
		Media:        matching.KindUnknown,
		Episodes:     []int{},
	}

	var seasons, parts, years []int
	var medias []matching.MediaKind
	allFinal := len(files) > 0
	for _, f := range files {
		if f.Episode.Kind == matching.NumberSingle && f.Episode.First > 0 {
			q.Episodes = append(q.Episodes, f.Episode.First)
		}
		if f.Ordinal.Season > 0 {
			seasons = append(seasons, f.Ordinal.Season)
		}
		if f.Ordinal.Part > 0 {
			parts = append(parts, f.Ordinal.Part)
		}
		if !f.Ordinal.Final {
			allFinal = false
		}
		if f.Year > 0 {
			years = append(years, f.Year)
		}
		if f.Media != "" && f.Media != matching.KindUnknown {
			medias = append(medias, f.Media)
		}
	}
	q.Episodes = distinctSortedPositive(q.Episodes)

	var conflicts []string
	conflicts = append(conflicts, hintFacts.Conflicts...)

	// Ordinal
	q.Ordinal.Part = hintFacts.Ordinal.Part
	if q.Ordinal.Part == 0 {
		if u := distinctSortedPositive(parts); len(u) == 1 {
			q.Ordinal.Part = u[0]
		}
	}
	q.Ordinal.Final = hintFacts.Ordinal.Final || allFinal
	fileSeasons := distinctSortedPositive(seasons)
	switch {
	case seasonHint > 0:
		q.Ordinal.Season = seasonHint
	case hintFacts.Ordinal.Season > 0:
		q.Ordinal.Season = hintFacts.Ordinal.Season
	case len(fileSeasons) == 1:
		q.Ordinal.Season = fileSeasons[0]
	case len(fileSeasons) > 1:
		conflicts = append(conflicts, fmt.Sprintf("组内季号冲突：%v", fileSeasons))
	}

	// Year / Media
	q.Year = hintFacts.Year
	if q.Year == 0 {
		if u := distinctSortedPositive(years); len(u) == 1 {
			q.Year = u[0]
		}
	}
	if hintFacts.Media != "" && hintFacts.Media != matching.KindUnknown {
		q.Media = hintFacts.Media
	} else if u := distinctMedia(medias); len(u) == 1 {
		q.Media = u[0]
	}

	// 文件自身冲突：SeasonHint>0 时不带入原始文件/目录季号冲突
	for _, f := range files {
		for _, c := range f.Conflicts {
			if seasonHint > 0 && isSeasonConflict(c) {
				continue
			}
			conflicts = append(conflicts, c)
		}
	}
	q.Conflicts = uniqueStrings(conflicts)
	if q.Conflicts == nil {
		q.Conflicts = []string{}
	}
	return q
}

// assistOffsetApplicable 每个 NumberSingle 文件的集号都必须大于 offset。
func assistOffsetApplicable(files []matching.FileEvidence, offset int) bool {
	for _, f := range files {
		if f.Episode.Kind != matching.NumberSingle {
			continue
		}
		if f.Episode.First <= offset {
			return false
		}
	}
	return true
}

// unshiftAssistFiles 还原已平移（且已通过 assistOffsetApplicable）的 NumberSingle 集号，供偏移适用性复核。
func unshiftAssistFiles(shifted []matching.FileEvidence, offset int) []matching.FileEvidence {
	out := make([]matching.FileEvidence, len(shifted))
	for i, f := range shifted {
		if f.Episode.Kind == matching.NumberSingle {
			f.Episode.First += offset
			f.Episode.Last += offset
		}
		out[i] = f
	}
	return out
}

func isSeasonConflict(c string) bool {
	return strings.Contains(c, "季号") && strings.Contains(c, "冲突")
}

func dropSeasonConflicts(in []string) []string {
	out := make([]string, 0, len(in))
	for _, c := range in {
		if isSeasonConflict(c) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func distinctSortedPositive(xs []int) []int {
	seen := map[int]bool{}
	out := []int{}
	for _, x := range xs {
		if x <= 0 || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	sort.Ints(out)
	return out
}

func distinctMedia(xs []matching.MediaKind) []matching.MediaKind {
	seen := map[matching.MediaKind]bool{}
	var out []matching.MediaKind
	for _, x := range xs {
		if seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}

func reviewDecision(reason string) matching.Decision {
	return matching.Decision{Code: matching.Review, Reason: reason, Candidates: []matching.Candidate{}}
}

func emptyAssistHintResult() AssistHintResult {
	return AssistHintResult{
		Decision: matching.Decision{Candidates: []matching.Candidate{}},
		Chapters: matching.ChapterDecision{
			Bindings: []matching.EpisodeBinding{},
			Rejected: map[string]string{},
		},
		Bindings:   []models.AssistMatchBinding{},
		Candidates: []models.MatchCandidate{},
	}
}

// modelCandidateFromSubject 独立构造 models.MatchCandidate（不复用 services.MatchCandidate）。
func modelCandidateFromSubject(sub matching.Subject, score float64) models.MatchCandidate {
	return models.MatchCandidate{
		ID:      sub.ID,
		Name:    sub.Name,
		NameCn:  sub.NameCn,
		Score:   score,
		Year:    sub.Year,
		EpCount: sub.EpisodeCount,
	}
}

func assistCandidatePtr(c models.MatchCandidate) *models.MatchCandidate {
	return &c
}

// assistBindings 章节绑定 → 按集号（同号按文件名）排序的 AssistMatchBinding。
func assistBindings(bindings []matching.EpisodeBinding) []models.AssistMatchBinding {
	out := make([]models.AssistMatchBinding, 0, len(bindings))
	for _, b := range bindings {
		if b.FileName == "" || b.StoredNumber <= 0 {
			continue
		}
		out = append(out, models.AssistMatchBinding{FileName: b.FileName, EpNumber: b.StoredNumber})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].EpNumber != out[j].EpNumber {
			return out[i].EpNumber < out[j].EpNumber
		}
		return out[i].FileName < out[j].FileName
	})
	return out
}
