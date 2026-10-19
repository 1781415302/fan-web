package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"fan-web/database"
	"fan-web/models"
	"fan-web/services/matching"
)

var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".mov": true,
	".flv": true, ".webm": true, ".ts": true, ".m4v": true,
}

var epPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\[(\d{1,3})(?:v\d+)?\]`),
	regexp.MustCompile(`\[[Ee](\d{1,3})(?:v\d+)?\]`),
	regexp.MustCompile(`_(\d{1,3})_`),
	regexp.MustCompile(`(?i)-\s*(\d{1,3})(?:v\d+)?\b`),
	regexp.MustCompile(`(?i)ep\.?\s*(\d{1,3})(?:v\d+)?\b`),
	regexp.MustCompile(`第\s*(\d{1,3})\s*[集話话]`),
	regexp.MustCompile(`(?i)S\d{1,2}E(\d{1,3})\b`),
	// RE2 无 lookbehind，用 (?:^|[^vV\d]) 等价于 (?<![vV\d])，避免把 v2.mkv 当成第 2 集。
	regexp.MustCompile(`(?i)(?:^|[^vV\d])(\d{1,3})(?:v\d+)?\.(?:mkv|mp4|avi|mov|flv|webm|ts|m4v)$`),
}

var ErrInvalidVideoPath = errors.New("文件目录必须是视频根目录下的相对目录")

type ScannerService struct {
	// matching 可选；nil 表示 legacy。由 WithScannerMatching 注入（S0 接缝）。
	matching *MatchingRuntime
	// rootMu 保护 rootPath：ScannerService 被 LibraryHandler.Dirs 等长期缓存并在
	// 请求间复用（每次 SetRootPath），并发请求与 SetRootPath 交错时不能对 string
	// 做无同步读写。SetRootPath 一次仅改一个字段，rootMu 足够；Scan 等长耗时
	// 操作在 resolveDirectory 内取一次快照，不全程持锁。
	rootMu   sync.RWMutex
	rootPath string
}

// ScannerOption 配置 ScannerService 的可选接缝。
type ScannerOption func(*ScannerService)

// WithScannerMatching 注入匹配 Runtime；无 opts 时保持 nil/legacy。
func WithScannerMatching(r *MatchingRuntime) ScannerOption {
	return func(s *ScannerService) { s.matching = r }
}

// NewScannerService 构造单番扫描服务。没有 opts 时完整 legacy。
func NewScannerService(rootPath string, opts ...ScannerOption) *ScannerService {
	s := &ScannerService{rootPath: rootPath}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// MatchEngine 返回注入的引擎；无 Runtime 时返回 Legacy（handler 不重读环境）。
func (s *ScannerService) MatchEngine() matching.Engine {
	if s == nil || s.matching == nil {
		return matching.EngineLegacy
	}
	return s.matching.Engine()
}

// SetRootPath 更新视频根目录，初始化完成时调用。
func (s *ScannerService) SetRootPath(rootPath string) {
	s.rootMu.Lock()
	defer s.rootMu.Unlock()
	s.rootPath = rootPath
}

// RootPath 返回当前视频根目录。
func (s *ScannerService) RootPath() string {
	s.rootMu.RLock()
	defer s.rootMu.RUnlock()
	return s.rootPath
}

func ValidateRelativeVideoPath(dirPath string) error {
	if strings.ContainsRune(dirPath, '\x00') {
		return ErrInvalidVideoPath
	}
	if filepath.IsAbs(dirPath) {
		return ErrInvalidVideoPath
	}
	// 反斜杠与正斜杠一视同仁：Windows 下库扫描/历史数据中的相对路径可能含 \，
	// 统一按文件系统分隔符解释后再做穿越校验（转换本身不会产生新的穿越风险，
	// 因为 Windows 上 filepath 同时识别两种分隔符，Linux 上 \ 只是普通文件名字符）。
	clean := filepath.Clean(strings.TrimSpace(filepath.FromSlash(dirPath)))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return ErrInvalidVideoPath
	}
	return nil
}

func (s *ScannerService) Scan(dirPath string) ([]models.Episode, error) {
	fullPath, err := s.resolveDirectory(dirPath)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("目录不存在")
		}
		return nil, fmt.Errorf("读取目录失败: %w", err)
	}

	names := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		name := entry.Name()
		if strings.Contains(name, ":Zone.Identifier") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if !videoExts[ext] {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	type scannerFile struct {
		name   string
		parsed ParsedFilename
	}
	videos := make([]scannerFile, 0, len(names))
	for _, name := range names {
		videos = append(videos, scannerFile{name: name, parsed: ParseFilename(name)})
	}

	seen := make(map[int]bool)
	episodes := make([]models.Episode, 0)
	for _, video := range videos {
		if video.parsed.EpisodeNum <= 0 || seen[video.parsed.EpisodeNum] {
			continue
		}
		seen[video.parsed.EpisodeNum] = true
		episodes = append(episodes, models.Episode{EpNumber: video.parsed.EpisodeNum, FilePath: video.name})
	}
	// 第二遍仅处理电影：电影默认映射到第 1 集，但仅在其目标集号未被第一遍的
	// 真实剧集占用时才落库。这样“真实集优先”不依赖 videos 的文件名排序结果。
	for _, video := range videos {
		if video.parsed.Kind != "movie" {
			continue
		}
		epNumber := libraryEpisodeNum(video.parsed)
		if seen[epNumber] {
			continue
		}
		seen[epNumber] = true
		episodes = append(episodes, models.Episode{EpNumber: epNumber, FilePath: video.name})
	}

	sort.Slice(episodes, func(i, j int) bool {
		return episodes[i].EpNumber < episodes[j].EpNumber
	})
	return episodes, nil
}

func (s *ScannerService) ListSubDirs() ([]string, error) {
	root, err := s.resolveDirectory(".")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("读取视频根目录失败: %w", err)
	}

	dirs := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || strings.HasPrefix(name, ".") || strings.Contains(name, ":Zone.Identifier") {
			continue
		}
		dirs = append(dirs, name)
	}
	sort.Strings(dirs)
	return dirs, nil
}

func (s *ScannerService) HasVideoFiles(dirPath string) (bool, error) {
	fullPath, err := s.resolveDirectory(dirPath)
	if err != nil {
		return false, err
	}
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return false, fmt.Errorf("读取目录失败: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || strings.Contains(entry.Name(), ":Zone.Identifier") {
			continue
		}
		if videoExts[strings.ToLower(filepath.Ext(entry.Name()))] {
			return true, nil
		}
	}
	return false, nil
}

func (s *ScannerService) DirectoryExists(dirPath string) bool {
	_, err := s.resolveDirectory(dirPath)
	return err == nil
}

// ResolveFilePath resolves a scanned episode file and keeps it inside dirPath.
func (s *ScannerService) ResolveFilePath(dirPath, fileName string) (string, error) {
	directory, err := s.resolveDirectory(dirPath)
	if err != nil {
		return "", err
	}
	if fileName == "" || strings.ContainsRune(fileName, '\x00') ||
		strings.Contains(fileName, "/") || strings.Contains(fileName, "\\") ||
		strings.Contains(fileName, "..") || filepath.IsAbs(fileName) || filepath.Base(fileName) != fileName {
		return "", ErrInvalidVideoPath
	}

	candidate := filepath.Join(directory, fileName)
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("视频文件不存在")
		}
		return "", fmt.Errorf("解析视频文件失败: %w", err)
	}
	relative, err := filepath.Rel(directory, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrInvalidVideoPath
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("读取视频文件失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("视频文件无效")
	}
	// 仅允许受支持的视频扩展名，避免流出字幕（.srt/.ass）、元数据（.nfo）等非媒体文件。
	if !videoExts[strings.ToLower(filepath.Ext(resolved))] {
		return "", fmt.Errorf("不支持的视频文件类型")
	}
	return resolved, nil
}

func (s *ScannerService) resolveDirectory(dirPath string) (string, error) {
	if err := ValidateRelativeVideoPath(dirPath); err != nil {
		return "", err
	}

	// 取一次根目录快照：整个解析只用同一个 root，避免解析中途 SetRootPath
	// 导致 root/target 各用一半不一致的根。
	s.rootMu.RLock()
	rootPath := s.rootPath
	s.rootMu.RUnlock()

	root, err := filepath.Abs(rootPath)
	if err != nil {
		return "", fmt.Errorf("解析视频根目录失败: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("视频根目录不存在")
		}
		return "", fmt.Errorf("解析视频根目录失败: %w", err)
	}

	clean := filepath.Clean(strings.TrimSpace(dirPath))
	if clean == "." {
		clean = ""
	}
	target := filepath.Join(root, clean)
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("目录不存在")
		}
		return "", fmt.Errorf("解析扫描目录失败: %w", err)
	}

	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrInvalidVideoPath
	}
	return resolved, nil
}

func extractEpisodeNumber(filename string) int {
	filename = normalizeFullwidthDigits(filename)
	for _, pattern := range epPatterns {
		match := pattern.FindStringSubmatch(filename)
		if len(match) < 2 {
			continue
		}
		var number int
		if _, err := fmt.Sscanf(match[1], "%d", &number); err == nil && number > 0 {
			return number
		}
	}
	return 0
}

// EpisodeInventory 单番 v2 重扫的安全清单（交给 ReconcileEpisodes）。
type EpisodeInventory struct {
	Desired      []models.Episode
	PresentFiles []string // 全部支持视频扩展名的实际文件，包括未选中/待确认文件
	Renames      []database.EpisodeRename
}

// scanSessionHook 仅测试注入 MatchingSession；nil 时用 Runtime.NewSession。
var scanSessionHook func(context.Context, *MatchingRuntime) *MatchingSession

// fileIsAssociated 关联检查端口；测试可替换，默认走 database.IsFileAssociated。
var fileIsAssociated = database.IsFileAssociated

// ScanForAnime 生成目标条目的安全 Inventory；生产 v2 单番重扫必须走此入口。
// 旧 Scan(dir) 语义不变。error 时调用方应零写入。
func (s *ScannerService) ScanForAnime(anime models.Anime, existing []models.Episode, peers []models.Anime) (EpisodeInventory, error) {
	empty := EpisodeInventory{
		Desired:      []models.Episode{},
		PresentFiles: []string{},
		Renames:      []database.EpisodeRename{},
	}
	if s == nil {
		return empty, fmt.Errorf("ScannerService 为空")
	}
	if s.matching == nil {
		return empty, fmt.Errorf("ScanForAnime 需要 MatchingRuntime（v2）")
	}
	if anime.BangumiID <= 0 {
		return empty, fmt.Errorf("目标番剧缺少有效 BangumiID")
	}

	fullPath, present, err := s.listPresentVideoFiles(anime.FilePath)
	if err != nil {
		return empty, err
	}
	_ = fullPath
	empty.PresentFiles = append([]string(nil), present...)

	presentSet := make(map[string]bool, len(present))
	for _, n := range present {
		presentSet[n] = true
	}

	existingByPath := make(map[string]models.Episode, len(existing))
	for _, ep := range existing {
		if ep.FilePath == "" {
			continue
		}
		existingByPath[ep.FilePath] = ep
	}

	var desired []models.Episode
	var missing []models.Episode

	for _, ep := range existing {
		if ep.FilePath == "" {
			continue
		}
		if presentSet[ep.FilePath] {
			kept := ep
			kept.EpNumber = correctExistingEpNumber(anime, ep)
			desired = append(desired, kept)
		} else {
			missing = append(missing, ep)
		}
	}

	// 未被目标 existing 占用的磁盘文件
	var free []string
	for _, name := range present {
		if _, owned := existingByPath[name]; owned {
			continue
		}
		associated, err := fileIsAssociated(name, anime.FilePath)
		if err != nil {
			return empty, fmt.Errorf("检查文件关联失败: %w", err)
		}
		if associated {
			// 已属于同目录其他 anime（或并发认领），不抢占
			continue
		}
		free = append(free, name)
	}

	catalog := matching.Catalog{}
	if s.matching != nil {
		catalog = s.matching.Catalog()
	}
	renames, claimedNew, _, identityConflict := proveEpisodeRenames(anime.FilePath, missing, free, catalog)
	if identityConflict {
		return empty, fmt.Errorf("%w: 同号文件替换无法证明作品身份", ErrMatchReviewRequired)
	}
	for _, r := range renames {
		old := existingByPath[r.From]
		ep := old
		ep.FilePath = r.To
		ep.EpNumber = correctExistingEpNumber(anime, models.Episode{
			ID: old.ID, AnimeID: old.AnimeID, EpNumber: old.EpNumber,
			Title: old.Title, FilePath: r.To, Duration: old.Duration,
		})
		desired = append(desired, ep)
	}
	var freeLeft []string
	for _, name := range free {
		if claimedNew[name] {
			continue
		}
		freeLeft = append(freeLeft, name)
	}

	// 剩余未关联文件：同一 Session 自动判断 → 其他 subject 忽略 → 其余 ResolveForTarget
	target := subjectFromAnime(anime)
	bound := &BoundMatch{
		Subject:         target,
		UniqueDirectory: len(peers) == 0,
	}

	var newBindings []matching.EpisodeBinding
	if len(freeLeft) > 0 {
		inputs := make([]matching.FileInput, 0, len(freeLeft))
		for _, name := range freeLeft {
			inputs = append(inputs, matching.FileInput{Name: name, Dir: anime.FilePath})
		}
		groups := matching.GroupFiles(inputs)
		sess := s.openMatchingSession(context.Background())
		if sess == nil || sess.provider == nil {
			return empty, fmt.Errorf("ScanForAnime: MatchingSession/Provider 不可用")
		}

		hadExistingTarget := len(existing) > 0
		for _, g := range groups {
			res, err := sess.Resolve(g, bound)
			if err != nil {
				return empty, err
			}
			if res.Decision.Code == matching.Accepted && res.Decision.WinnerID > 0 && res.Decision.WinnerID != anime.BangumiID {
				// 明确属于其他作品，忽略并留在收件箱
				continue
			}
			if res.Decision.Code == matching.Accepted && res.Decision.WinnerID == anime.BangumiID {
				newBindings = append(newBindings, res.Chapters.Bindings...)
				continue
			}

			resT, err := sess.ResolveForTarget(g, target)
			if err != nil {
				return empty, err
			}
			if resT.Decision.Code == matching.Accepted && resT.Decision.WinnerID == anime.BangumiID {
				newBindings = append(newBindings, resT.Chapters.Bindings...)
				continue
			}

			// 未接受：判断是否「目标相关仍冲突」→ ReviewRequired，还是低身份支持 → 忽略
			if scanGroupNeedsReview(resT, anime.BangumiID, hadExistingTarget) {
				return empty, fmt.Errorf("%w: %s", ErrMatchReviewRequired, reviewReason(resT))
			}
		}
	}

	claimedPaths := map[string]bool{}
	for _, ep := range desired {
		claimedPaths[ep.FilePath] = true
	}
	for _, b := range newBindings {
		if b.FileName == "" || b.StoredNumber <= 0 {
			continue
		}
		if claimedPaths[b.FileName] {
			// 已由 existing/rename 占用；跨组重复交由下方查重
			continue
		}
		desired = append(desired, models.Episode{
			ID:       0,
			AnimeID:  anime.ID,
			EpNumber: b.StoredNumber,
			FilePath: b.FileName,
		})
		claimedPaths[b.FileName] = true
	}

	if err := validateDesiredUnique(desired, newBindings); err != nil {
		return empty, err
	}

	// 全新目标没有任何可加入文件 → ReviewRequired（不伪报扫描成功）
	if len(existing) == 0 && len(desired) == 0 {
		return empty, fmt.Errorf("%w: 全新目标没有可关联的文件", ErrMatchReviewRequired)
	}

	sort.SliceStable(desired, func(i, j int) bool {
		if desired[i].EpNumber != desired[j].EpNumber {
			return desired[i].EpNumber < desired[j].EpNumber
		}
		return desired[i].FilePath < desired[j].FilePath
	})

	return EpisodeInventory{
		Desired:      desired,
		PresentFiles: append([]string(nil), present...),
		Renames:      renames,
	}, nil
}

func (s *ScannerService) openMatchingSession(ctx context.Context) *MatchingSession {
	if scanSessionHook != nil {
		return scanSessionHook(ctx, s.matching)
	}
	if s.matching == nil {
		return nil
	}
	return s.matching.NewSession(ctx)
}

func (s *ScannerService) listPresentVideoFiles(dirPath string) (fullPath string, names []string, err error) {
	fullPath, err = s.resolveDirectory(dirPath)
	if err != nil {
		return "", nil, err
	}
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, fmt.Errorf("目录不存在")
		}
		return "", nil, fmt.Errorf("读取目录失败: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		name := entry.Name()
		if strings.Contains(name, ":Zone.Identifier") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if !videoExts[ext] {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return fullPath, names, nil
}

func subjectFromAnime(a models.Anime) matching.Subject {
	return matching.Subject{
		ID:           a.BangumiID,
		Name:         a.Title,
		NameCn:       a.TitleCn,
		EpisodeCount: a.EpCount,
		Detailed:     false,
	}
}

// correctExistingEpNumber 仅在新解析明确、无硬冲突、且对目标话数可解释时更正集号；否则原样保留。
func correctExistingEpNumber(anime models.Anime, ep models.Episode) int {
	ev := matching.ParseFile(matching.FileInput{Name: ep.FilePath, Dir: anime.FilePath})
	if len(ev.Conflicts) > 0 {
		return ep.EpNumber
	}
	switch ev.Episode.Kind {
	case matching.NumberSingle:
		n := ev.Episode.First
		if n <= 0 {
			return ep.EpNumber
		}
		if anime.EpCount > 0 && n > anime.EpCount {
			// 越界不静默更正
			return ep.EpNumber
		}
		return n
	case matching.NumberUnknown:
		if ev.Media == matching.KindMovie {
			// 明确电影且无真实编号时允许保持/落到 1
			if ep.EpNumber > 0 {
				return ep.EpNumber
			}
			return 1
		}
		return ep.EpNumber
	default:
		// Range/Fraction/Special：不更正
		return ep.EpNumber
	}
}

// proveEpisodeRenames 仅在「旧不在、新在、明确集号一致、季/类型无冲突、唯一一对一、标题身份可证」时产生 rename。
// 同号结构唯一配对但无法证明同一作品时 identityConflict=true，调用方应零写入并 Review。
func proveEpisodeRenames(dir string, missing []models.Episode, free []string, catalog matching.Catalog) (renames []database.EpisodeRename, claimedNew, claimedMissing map[string]bool, identityConflict bool) {
	claimedNew = map[string]bool{}
	claimedMissing = map[string]bool{}
	if len(missing) == 0 || len(free) == 0 {
		return nil, claimedNew, claimedMissing, false
	}

	type cand struct {
		old   models.Episode
		neu   string
		num   int
		evOld matching.FileEvidence
		evNew matching.FileEvidence
	}
	var pairs []cand
	for _, old := range missing {
		oldEv := matching.ParseFile(matching.FileInput{Name: old.FilePath, Dir: dir})
		oldNum, ok := explicitSingleNumber(oldEv)
		if !ok {
			continue
		}
		for _, neu := range free {
			newEv := matching.ParseFile(matching.FileInput{Name: neu, Dir: dir})
			newNum, ok := explicitSingleNumber(newEv)
			if !ok || newNum != oldNum {
				continue
			}
			if evidenceStructureConflict(oldEv, newEv) {
				continue
			}
			pairs = append(pairs, cand{old: old, neu: neu, num: oldNum, evOld: oldEv, evNew: newEv})
		}
	}

	// 唯一一对一：旧或新出现多次则整组丢弃该端的全部候选
	oldCount := map[string]int{}
	newCount := map[string]int{}
	for _, p := range pairs {
		oldCount[p.old.FilePath]++
		newCount[p.neu]++
	}
	var unique []cand
	for _, p := range pairs {
		if oldCount[p.old.FilePath] != 1 || newCount[p.neu] != 1 {
			continue
		}
		unique = append(unique, p)
	}

	for _, p := range unique {
		if !matching.FileEvidenceShareWork(p.evOld, p.evNew, catalog) {
			// 同号唯一替换却无法证明作品身份 → 整次改名作废并上报冲突
			return nil, map[string]bool{}, map[string]bool{}, true
		}
	}

	for _, p := range unique {
		if claimedMissing[p.old.FilePath] || claimedNew[p.neu] {
			continue
		}
		claimedMissing[p.old.FilePath] = true
		claimedNew[p.neu] = true
		renames = append(renames, database.EpisodeRename{
			EpisodeID: p.old.ID,
			From:      p.old.FilePath,
			To:        p.neu,
		})
	}
	sort.SliceStable(renames, func(i, j int) bool {
		return renames[i].From < renames[j].From
	})
	return renames, claimedNew, claimedMissing, false
}

func explicitSingleNumber(ev matching.FileEvidence) (int, bool) {
	if len(ev.Conflicts) > 0 {
		return 0, false
	}
	if ev.Episode.Kind != matching.NumberSingle || ev.Episode.First <= 0 {
		return 0, false
	}
	return ev.Episode.First, true
}

func evidenceStructureConflict(a, b matching.FileEvidence) bool {
	if a.Ordinal.Season > 0 && b.Ordinal.Season > 0 && a.Ordinal.Season != b.Ordinal.Season {
		return true
	}
	if a.Ordinal.Part > 0 && b.Ordinal.Part > 0 && a.Ordinal.Part != b.Ordinal.Part {
		return true
	}
	if a.Ordinal.Final != b.Ordinal.Final && (a.Ordinal.Final || b.Ordinal.Final) {
		// 一侧明确 Final、另一侧不是 → 冲突
		if a.Ordinal.Season > 0 || b.Ordinal.Season > 0 || a.Ordinal.Part > 0 || b.Ordinal.Part > 0 || a.Ordinal.Final || b.Ordinal.Final {
			if a.Ordinal.Final != b.Ordinal.Final {
				return true
			}
		}
	}
	if a.Media != matching.KindUnknown && b.Media != matching.KindUnknown && a.Media != b.Media {
		return true
	}
	if a.Year > 0 && b.Year > 0 {
		diff := a.Year - b.Year
		if diff < 0 {
			diff = -diff
		}
		if diff >= 2 {
			return true
		}
	}
	return false
}

func scanGroupNeedsReview(res MatchResult, targetID int, hadExistingTarget bool) bool {
	if res.Decision.Code == matching.Accepted {
		return false
	}
	// 章节冲突且目标相关
	if res.Chapters.State == matching.ChapterConflict {
		if targetNameScore(res, targetID) >= 0.70 || hadExistingTarget {
			return true
		}
	}
	score := targetNameScore(res, targetID)
	if score >= 0.70 {
		// 身份够但硬冲突/编号歧义/未确认 → 中止，不部分写入
		return true
	}
	return false
}

func targetNameScore(res MatchResult, targetID int) float64 {
	best := 0.0
	for _, c := range res.Decision.Candidates {
		if c.Subject.ID == targetID && c.NameScore > best {
			best = c.NameScore
		}
	}
	return best
}

func reviewReason(res MatchResult) string {
	if res.Decision.Reason != "" {
		return res.Decision.Reason
	}
	if res.Chapters.State == matching.ChapterConflict {
		return "章节或编号冲突，需要人工确认"
	}
	return "匹配需要人工确认"
}

func validateDesiredUnique(desired []models.Episode, bindings []matching.EpisodeBinding) error {
	byNum := map[int][]string{}
	for _, ep := range desired {
		if ep.EpNumber <= 0 {
			continue
		}
		byNum[ep.EpNumber] = append(byNum[ep.EpNumber], ep.FilePath)
	}
	for num, files := range byNum {
		if len(files) > 1 {
			return fmt.Errorf("%w: 重复集号 %d 映射多个文件 %v", ErrMatchReviewRequired, num, files)
		}
	}
	byChapter := map[int][]string{}
	for _, b := range bindings {
		if b.ChapterID == 0 {
			continue
		}
		byChapter[b.ChapterID] = append(byChapter[b.ChapterID], b.FileName)
	}
	for cid, files := range byChapter {
		uniq := uniqueStrings(files)
		if len(uniq) > 1 {
			return fmt.Errorf("%w: 多个文件映射同一 ChapterID %d: %v", ErrMatchReviewRequired, cid, uniq)
		}
	}
	return nil
}

func uniqueStrings(in []string) []string {
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
