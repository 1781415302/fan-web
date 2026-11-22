package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"

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
	// 反斜杠与正斜杠一视同仁：显式把 \ 转成 / 后再检查前导斜杠、盘符与 .. 段。
	// Linux 上 filepath.FromSlash 不会替换 \，若依赖它会让 "..\\outside" 绕过穿越校验。
	slashed := strings.ReplaceAll(strings.TrimSpace(dirPath), "\\", "/")
	if strings.HasPrefix(slashed, "/") {
		return ErrInvalidVideoPath
	}
	if len(slashed) >= 2 && unicode.IsLetter(rune(slashed[0])) && slashed[1] == ':' {
		return ErrInvalidVideoPath
	}
	for _, seg := range strings.Split(slashed, "/") {
		if strings.TrimSpace(seg) == ".." {
			return ErrInvalidVideoPath
		}
	}
	return nil
}

// NormalizeRelDir 把相对目录统一为以 / 分隔的规范形式：TrimSpace → \ 转 / → path.Clean
// （"." 归一为 ""）→ 去掉前导与末尾 /。"a\\b/" 与 "a/b" 同为 "a/b"，根目录为 ""。
func NormalizeRelDir(dirPath string) string {
	s := strings.ReplaceAll(strings.TrimSpace(dirPath), "\\", "/")
	if s == "" {
		return ""
	}
	s = path.Clean(s)
	if s == "." {
		return ""
	}
	return strings.Trim(s, "/")
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

	clean := filepath.Clean(filepath.FromSlash(NormalizeRelDir(dirPath)))
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

// ScanOption 调整单次 ScanForAnime 的行为；不传选项与旧行为等价。
type ScanOption func(*scanOptions)

type scanOptions struct {
	// onlyFiles 非 nil 时仅允许其中的文件进入未关联待匹配集；nil=不限定。
	onlyFiles map[string]bool
	// episodeOffset >0 时在 GroupFiles 之后、Resolve 之前平移 Single 集号。
	episodeOffset int
	// trustedTarget 为 true 时目标条目已由外部确定（anime.BangumiID>0）：
	// 跳过名称门闩，但仍强制详情 + 章节绑定、跨番隔离与文件合法性校验。
	trustedTarget bool
	// trustedBindings 非 nil 时（仅配合 trustedTarget）：未关联文件直接采用同一次请求预览得出的章节绑定，
	// 不再重新解释章节；仍须逐个文件命中绑定，且照常经过归属过滤、查重与 Reconcile 快照校验。
	trustedBindings []matching.EpisodeBinding
}

// errEpisodeOffsetNotApplicable 偏移适用性不满足时的说明（包装 ErrMatchReviewRequired）。
const errEpisodeOffsetNotApplicable = "集号偏移仅适用于累计编号（集号需大于该条目话数）"

// WithOnlyFiles 仅限定未关联文件集 free；已有剧集绑定照常保留。
// names 为目录内文件名；空切片=不限定。
func WithOnlyFiles(names []string) ScanOption {
	return func(o *scanOptions) {
		if len(names) == 0 {
			o.onlyFiles = nil
			return
		}
		set := make(map[string]bool, len(names))
		for _, n := range names {
			set[n] = true
		}
		o.onlyFiles = set
	}
}

// WithEpisodeOffset n>=0；n==0 与不传等价。
// n>0 仅适用于累计编号：anime.EpCount>0 且被平移文件的原始集号全部 > anime.EpCount，
// 否则 ScanForAnime 返回 ErrMatchReviewRequired 包装错误。
func WithEpisodeOffset(n int) ScanOption {
	return func(o *scanOptions) {
		if n < 0 {
			n = 0
		}
		o.episodeOffset = n
	}
}

// WithTrustedTarget 启用受信任目标模式：仅配合已由外部确定 anime.BangumiID>0 使用。
// 未关联文件不再走自动识别/名称门闩，直接按目标条目详情与章节解释集号；
// 任一文件无法被章节解释（冲突/拒绝/无集号被跳过）即整体 ErrMatchReviewRequired。
func WithTrustedTarget() ScanOption {
	return func(o *scanOptions) {
		o.trustedTarget = true
	}
}

// WithTrustedBindings 受信任目标模式下复用同一次请求预览（ResolveAssistByHint）已得出的章节绑定，
// 保证预览与提交使用相同的集号映射（如季锚点 season_sort）。bindings 已含 episode_offset 平移结果。
// 空切片等同未设置（回退为按目标章节重新解释）。
func WithTrustedBindings(bindings []matching.EpisodeBinding) ScanOption {
	return func(o *scanOptions) {
		if len(bindings) == 0 {
			o.trustedBindings = nil
			return
		}
		o.trustedBindings = append([]matching.EpisodeBinding(nil), bindings...)
	}
}

// episodeOffsetApplicable 偏移适用性：被平移（NumberSingle）文件的原始集号必须全部 > epCount（epCount>0）。
func episodeOffsetApplicable(g matching.Group, epCount int) bool {
	if epCount <= 0 {
		return false
	}
	for _, f := range g.Files {
		if f.Episode.Kind != matching.NumberSingle {
			continue
		}
		if f.Episode.First <= epCount || f.Episode.Last <= epCount {
			return false
		}
	}
	return true
}

// assistEpisodeOffsetReason 辅助匹配预览（PreviewAssistByBangumiID / ResolveAssistByHint）与受信任提交
// （trustedTargetBindings）共用的偏移校验，保证同一输入两边结论一致。files 为未平移的原始证据：
// 先要求每个 NumberSingle 集号 > offset，再要求 episodeOffsetApplicable（话数已知且集号 > 话数）。
// 返回空串表示适用；offset<=0 不校验。
func assistEpisodeOffsetReason(files []matching.FileEvidence, offset, epCount int) string {
	if offset <= 0 {
		return ""
	}
	if !assistOffsetApplicable(files, offset) {
		return errAssistOffsetTooLarge
	}
	if !episodeOffsetApplicable(matching.Group{Files: files}, epCount) {
		return errEpisodeOffsetNotApplicable
	}
	return ""
}

// ScanForAnime 生成目标条目的安全 Inventory；生产 v2 单番重扫必须走此入口。
// 旧 Scan(dir) 语义不变。error 时调用方应零写入。
// opts：WithOnlyFiles 限定未关联文件集；WithEpisodeOffset 在分组后平移集号（仅累计编号）。
func (s *ScannerService) ScanForAnime(anime models.Anime, existing []models.Episode, peers []models.Anime, opts ...ScanOption) (EpisodeInventory, error) {
	empty := EpisodeInventory{
		Desired:      []models.Episode{},
		PresentFiles: []string{},
		Renames:      []database.EpisodeRename{},
	}
	var options scanOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
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
	if options.episodeOffset > 0 && anime.EpCount <= 0 {
		return empty, fmt.Errorf("%w: %s", ErrMatchReviewRequired, errEpisodeOffsetNotApplicable)
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

	// 会话按需打开：既有剧集更正校验与未关联文件匹配共用同一个 MatchingSession。
	var sess *MatchingSession
	getSession := func() *MatchingSession {
		if sess == nil {
			sess = s.openMatchingSession(context.Background())
		}
		return sess
	}
	// 话数未知（EpCount<=0）时“n 对目标话数可解释”无从判断：改为用目标章节核对，
	// 只有文件号 n 能按原 ep/sort 直接对上目标章节才允许更正，否则沿用已保存集号
	// （季锚点 / 总集数换算得到的条目内集号不会被改回文件号）。章节取不到时同样保守沿用。
	var targetNumbers map[int]bool
	targetNumbersLoaded := false
	correctKept := func(ep models.Episode) int {
		n := correctExistingEpNumber(anime, ep)
		if n == ep.EpNumber || anime.EpCount > 0 {
			return n
		}
		if !targetNumbersLoaded {
			targetNumbersLoaded = true
			targetNumbers = targetChapterNumbers(getSession(), anime.BangumiID)
		}
		if targetNumbers[n] {
			return n
		}
		return ep.EpNumber
	}

	for _, ep := range existing {
		if ep.FilePath == "" {
			continue
		}
		if presentSet[ep.FilePath] {
			kept := ep
			kept.EpNumber = correctKept(ep)
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
		ep.EpNumber = correctKept(models.Episode{
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
		if options.onlyFiles != nil && !options.onlyFiles[name] {
			// WithOnlyFiles：仅限定待匹配的未关联文件；已有剧集（含可证明的改名）照常保留
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
	if len(freeLeft) > 0 && options.trustedTarget {
		var bindings []matching.EpisodeBinding
		var err error
		if options.trustedBindings != nil {
			bindings, err = presetTrustedBindings(anime, freeLeft, options.trustedBindings, options.episodeOffset)
		} else {
			bindings, err = trustedTargetBindings(anime, freeLeft, getSession(), options.episodeOffset)
		}
		if err != nil {
			return empty, err
		}
		newBindings = bindings
	} else if len(freeLeft) > 0 {
		inputs := make([]matching.FileInput, 0, len(freeLeft))
		for _, name := range freeLeft {
			inputs = append(inputs, matching.FileInput{Name: name, Dir: anime.FilePath})
		}
		groups := matching.GroupFiles(inputs)
		sess := getSession()
		if sess == nil || sess.provider == nil {
			return empty, fmt.Errorf("ScanForAnime: MatchingSession/Provider 不可用")
		}

		hadExistingTarget := len(existing) > 0
		for _, g := range groups {
			if options.episodeOffset > 0 {
				if !episodeOffsetApplicable(g, anime.EpCount) {
					return empty, fmt.Errorf("%w: %s", ErrMatchReviewRequired, errEpisodeOffsetNotApplicable)
				}
				g = matching.ShiftGroupEpisodes(g, options.episodeOffset)
			}
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

// trustedTargetBindings 受信任目标模式下解释未关联文件（§4.5 ScanForAnime 受信任规则 2–5）：
// 逐个构建 FileEvidence → 可选集号偏移 → 目标详情 → 章节解释 → 全量绑定校验。
func trustedTargetBindings(anime models.Anime, freeLeft []string, sess *MatchingSession, episodeOffset int) ([]matching.EpisodeBinding, error) {
	evs := make([]matching.FileEvidence, 0, len(freeLeft))
	for _, name := range freeLeft {
		evs = append(evs, matching.ParseFile(matching.FileInput{Name: name, Dir: anime.FilePath}))
	}
	g := matching.Group{Files: evs}
	if episodeOffset > 0 {
		if reason := assistEpisodeOffsetReason(evs, episodeOffset, anime.EpCount); reason != "" {
			return nil, fmt.Errorf("%w: %s", ErrMatchReviewRequired, reason)
		}
		g = matching.ShiftGroupEpisodes(g, episodeOffset)
	}
	shiftedFiles := g.Files
	if sess == nil || sess.provider == nil {
		return nil, fmt.Errorf("ScanForAnime: MatchingSession/Provider 不可用")
	}
	_, ch, err := interpretTrustedTargetChapters(sess, anime.BangumiID, shiftedFiles)
	if err != nil {
		return nil, err
	}
	if reason := trustedChapterReviewReason(ch, shiftedFiles); reason != "" {
		return nil, fmt.Errorf("%w: %s", ErrMatchReviewRequired, reason)
	}
	return ch.Bindings, nil
}

// presetTrustedBindings 受信任目标模式下采用预览阶段的章节绑定：偏移适用性与 trustedTargetBindings 同口径校验，
// 每个未关联文件必须恰好命中一条 StoredNumber>0 的绑定，否则整体 ErrMatchReviewRequired。
func presetTrustedBindings(anime models.Anime, freeLeft []string, preset []matching.EpisodeBinding, episodeOffset int) ([]matching.EpisodeBinding, error) {
	if episodeOffset > 0 {
		evs := make([]matching.FileEvidence, 0, len(freeLeft))
		for _, name := range freeLeft {
			evs = append(evs, matching.ParseFile(matching.FileInput{Name: name, Dir: anime.FilePath}))
		}
		if reason := assistEpisodeOffsetReason(evs, episodeOffset, anime.EpCount); reason != "" {
			return nil, fmt.Errorf("%w: %s", ErrMatchReviewRequired, reason)
		}
	}
	out, missing := bindingsForEveryFile(freeLeft, preset)
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrMatchReviewRequired, missingBindingsReason(missing))
	}
	return out, nil
}

// bindingsForEveryFile 辅助匹配预览与受信任提交共用的“每个文件都必须有有效绑定”检查：
// files 中每个文件须恰好命中一条 StoredNumber>0 的绑定。返回按 files 顺序的绑定，以及未满足的文件。
func bindingsForEveryFile(files []string, bindings []matching.EpisodeBinding) ([]matching.EpisodeBinding, []string) {
	byName := make(map[string]matching.EpisodeBinding, len(bindings))
	dup := map[string]bool{}
	for _, b := range bindings {
		if _, ok := byName[b.FileName]; ok {
			dup[b.FileName] = true
		}
		byName[b.FileName] = b
	}
	out := make([]matching.EpisodeBinding, 0, len(files))
	var missing []string
	for _, name := range files {
		b, ok := byName[name]
		if !ok || dup[name] || b.StoredNumber <= 0 {
			missing = append(missing, name)
			continue
		}
		out = append(out, b)
	}
	return out, missing
}

// missingBindingsReason 缺少有效绑定时的复核原因。
func missingBindingsReason(missing []string) string {
	return "以下文件无法确定集号：" + strings.Join(missing, "、")
}

// interpretTrustedTargetChapters 取目标条目详情与章节，并按受信任模式解释文件集号（零 DB 写）。
// 绕过 interpretWithAnchor 季锚点推算：Bangumi 累计编号季番须由调用方传 episode_offset。
// 条目 404 → ErrMatchReviewRequired；章节 404 且话数已知 → 按话数拟合；
// 章节 404 且话数未知 → ErrMatchReviewRequired；其他上游错误原样返回。
func interpretTrustedTargetChapters(sess *MatchingSession, bangumiID int, files []matching.FileEvidence) (matching.Subject, matching.ChapterDecision, error) {
	if sess.ctx == nil {
		sess.ctx = context.Background()
	}
	if err := sess.ensureDetailed(bangumiID); err != nil {
		if isSubjectNotFound(err) {
			return matching.Subject{}, matching.ChapterDecision{}, fmt.Errorf("%w: 目标条目不存在或不可访问", ErrMatchReviewRequired)
		}
		return matching.Subject{}, matching.ChapterDecision{}, err
	}
	detailedSub := sess.pool[bangumiID]
	chs, chErr := sess.provider.Chapters(sess.ctx, bangumiID)
	switch {
	case chErr == nil:
		return detailedSub, matching.InterpretChapters(files, detailedSub, chs, true), nil
	case isSubjectNotFound(chErr) && detailedSub.EpisodeCount > 0:
		return detailedSub, matching.InterpretChapters(files, detailedSub, nil, true), nil
	case isSubjectNotFound(chErr):
		return detailedSub, matching.ChapterDecision{}, fmt.Errorf("%w: 目标条目无章节且话数未知", ErrMatchReviewRequired)
	default:
		return detailedSub, matching.ChapterDecision{}, chErr
	}
}

// trustedChapterReviewReason 受信任模式绝不静默跳过：章节冲突、任一文件被拒绝，
// 或绑定数与文件数不一致（InterpretChapters 对无集号非电影文件既不绑定也不拒绝）时返回原因。
func trustedChapterReviewReason(ch matching.ChapterDecision, files []matching.FileEvidence) string {
	if ch.State != matching.ChapterConflict && len(ch.Rejected) == 0 && len(ch.Bindings) == len(files) {
		return ""
	}
	if len(ch.Rejected) > 0 {
		names := make([]string, 0, len(ch.Rejected))
		for name := range ch.Rejected {
			names = append(names, name)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, name := range names {
			parts = append(parts, fmt.Sprintf("%s（%s）", name, ch.Rejected[name]))
		}
		return "章节无法解释文件：" + strings.Join(parts, "；")
	}
	bound := make(map[string]bool, len(ch.Bindings))
	for _, b := range ch.Bindings {
		bound[b.FileName] = true
	}
	var missing []string
	for _, f := range files {
		if !bound[f.Name] {
			missing = append(missing, f.Name)
		}
	}
	if len(missing) > 0 {
		return "以下文件无法确定集号：" + strings.Join(missing, "、")
	}
	return "章节或编号冲突，需要人工确认"
}

// targetChapterNumbers 目标条目章节中可作为“文件原始号直接命中”的编号集合（整数 Ep 与整数 Sort，>=1），
// 与 InterpretChapters 的 ep/sort 基准一致。会话或章节不可用时返回 nil（调用方保守沿用已保存集号）。
func targetChapterNumbers(sess *MatchingSession, bangumiID int) map[int]bool {
	if sess == nil || sess.provider == nil || bangumiID <= 0 {
		return nil
	}
	chs, err := sess.provider.Chapters(sess.ctx, bangumiID)
	if err != nil {
		return nil
	}
	out := make(map[int]bool, len(chs)*2)
	for _, ch := range chs {
		for _, v := range []float64{ch.Ep, ch.Sort} {
			if v >= 1 && v == math.Trunc(v) {
				out[int(v)] = true
			}
		}
	}
	return out
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
	return correctExistingEpNumberFromEvidence(anime, ep, ev)
}

// correctExistingEpNumberFromEvidence 为 correctExistingEpNumber 的判定主体（ev 为该文件的解析证据）。
func correctExistingEpNumberFromEvidence(anime models.Anime, ep models.Episode, ev matching.FileEvidence) int {
	if len(ev.Conflicts) > 0 {
		return ep.EpNumber
	}
	if ev.AbsEpisode > 0 {
		// 总集数标注意味着文件号与条目号可能不同，不得按文件名回改
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
