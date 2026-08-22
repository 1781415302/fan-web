package services

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"fan-web/database"
	"fan-web/models"
)

type LibraryService struct {
	bangumi  *BangumiService
	rootPath string
	// scanMu 串行化库扫描，防止两个并发 Scan 对同一分组"先查后插"交错写入，
	// 与数据库唯一索引共同保证不产生重复番剧/剧集。
	scanMu sync.Mutex
}

type LibraryScanResult struct {
	TotalFiles   int                `json:"total_files"`
	Skipped      int                `json:"skipped"`
	NewAnimes    int                `json:"new_animes"`
	NewEpisodes  int                `json:"new_episodes"`
	Unidentified []UnidentifiedFile `json:"unidentified"`
}

type UnidentifiedFile struct {
	FileName   string           `json:"file_name"`
	Reason     string           `json:"reason"`
	FilePath   string           `json:"file_path"`
	Candidates []MatchCandidate `json:"candidates"`
}

type libraryFile struct {
	fileName string
	relDir   string
	fullPath string
}

type parsedLibraryFile struct {
	libraryFile
	parsed ParsedFilename
}

func NewLibraryService(bangumi *BangumiService, rootPath string) *LibraryService {
	return &LibraryService{bangumi: bangumi, rootPath: rootPath}
}

// SetRootPath 更新视频根目录，初始化完成时调用。
func (s *LibraryService) SetRootPath(rootPath string) {
	s.rootPath = rootPath
}

// RootPath 返回当前视频根目录，供 handler 接线 ListSubDirs。
func (s *LibraryService) RootPath() string {
	return s.rootPath
}

// groupKey 是扫描分组键：同目录同有效标题为一组。
type groupKey struct {
	relDir string
	title  string // effectiveTitle（含季号合成后缀 "第N季"）
}

// scanContext 单次扫描的共享状态。processGroup 串行调用（Scan 持 scanMu），无需锁。
type scanContext struct {
	episodesBySubject map[int]subjectEpisodesResult // subjectID → 本篇剧集（记忆化）
}

type subjectEpisodesResult struct {
	episodes []BangumiEpisode
	err      error // 失败也缓存：同次扫描不重复打失败 API
}

// boundTitleMinScore 快通道标题冲突闸，对所有组统一适用。
const boundTitleMinScore = 0.5

func effectiveTitle(parsed ParsedFilename, relDir string) string {
	effTitle := parsed.Title
	if effTitle != "" {
		return effTitle
	}
	hint := DeriveDirTitle(relDir)
	if hint.Title == "" {
		return ""
	}
	season := parsed.Season
	if season == 0 {
		season = hint.Season
	}
	if season > 0 {
		return fmt.Sprintf("%s 第%d季", hint.Title, season)
	}
	return hint.Title
}

func stripSeasonSuffix(title string) string {
	t := stripAll(title, matchSeasonRe)
	t = strings.Join(strings.Fields(t), " ")
	return strings.Trim(t, " -")
}

func (s *LibraryService) Scan() (*LibraryScanResult, error) {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	result := &LibraryScanResult{Unidentified: make([]UnidentifiedFile, 0)}
	allFiles, err := s.collectFiles()
	if err != nil {
		return nil, err
	}
	result.TotalFiles = len(allFiles)

	// 先解析每一个视频（含已关联），按 (relDir, effectiveTitle) 标记真集号 1。
	hasRealEp1 := make(map[groupKey]struct{})
	parsedFiles := make([]parsedLibraryFile, 0, len(allFiles))
	for _, file := range allFiles {
		parsed := ParseFilename(file.fileName)
		eff := effectiveTitle(parsed, file.relDir)
		if parsed.EpisodeNum == 1 && eff != "" {
			hasRealEp1[groupKey{relDir: file.relDir, title: eff}] = struct{}{}
		}
		parsedFiles = append(parsedFiles, parsedLibraryFile{libraryFile: file, parsed: parsed})
	}

	groups := make(map[groupKey][]parsedLibraryFile)
	for _, file := range parsedFiles {
		associated, err := database.IsFileAssociated(file.fileName, file.relDir)
		if err != nil {
			log.Printf("[Library] 查询文件关联状态失败 %q: %v", file.fileName, err)
			addUnidentified(result, file.fileName, file.relDir, "查询文件状态失败")
			continue
		}
		if associated {
			result.Skipped++
			continue
		}

		eff := effectiveTitle(file.parsed, file.relDir)
		if eff == "" {
			addUnidentified(result, file.fileName, file.relDir, "无法解析文件名")
			continue
		}
		if file.parsed.EpisodeNum == 0 {
			if _, marked := hasRealEp1[groupKey{relDir: file.relDir, title: eff}]; marked {
				addUnidentified(result, file.fileName, file.relDir, "同目录已有第 1 集")
				continue
			}
		}
		if file.parsed.EpisodeNum > 0 || file.parsed.Kind == "movie" || isStrongTitle(eff) {
			key := groupKey{relDir: file.relDir, title: eff}
			groups[key] = append(groups[key], file)
			continue
		}
		addUnidentified(result, file.fileName, file.relDir, "无法识别集数")
	}

	keys := make([]groupKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].relDir != keys[j].relDir {
			return keys[i].relDir < keys[j].relDir
		}
		return keys[i].title < keys[j].title
	})
	ctx := &scanContext{episodesBySubject: make(map[int]subjectEpisodesResult)}
	for _, key := range keys {
		s.processGroup(key, groups[key], ctx, result)
	}
	return result, nil
}

func (s *LibraryService) collectFiles() ([]libraryFile, error) {
	files := make([]libraryFile, 0)
	err := filepath.WalkDir(s.rootPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		name := entry.Name()
		if strings.Contains(name, ":Zone.Identifier") || !videoExts[strings.ToLower(filepath.Ext(name))] {
			return nil
		}
		relPath, err := filepath.Rel(s.rootPath, path)
		if err != nil {
			return err
		}
		relDir := filepath.Dir(relPath)
		if relDir == "." {
			relDir = ""
		}
		files = append(files, libraryFile{fileName: name, relDir: relDir, fullPath: path})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("遍历视频根目录失败: %w", err)
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].relDir != files[j].relDir {
			return files[i].relDir < files[j].relDir
		}
		return files[i].fileName < files[j].fileName
	})
	return files, nil
}

func (s *LibraryService) subjectEpisodes(ctx *scanContext, subjectID int) ([]BangumiEpisode, error) {
	if ctx != nil {
		if cached, ok := ctx.episodesBySubject[subjectID]; ok {
			return cached.episodes, cached.err
		}
	}
	var episodes []BangumiEpisode
	var err error
	if s.bangumi == nil {
		err = fmt.Errorf("Bangumi 服务不可用")
	} else {
		episodes, err = s.bangumi.ListPublicSubjectEpisodes(subjectID)
	}
	if ctx != nil {
		if ctx.episodesBySubject == nil {
			ctx.episodesBySubject = make(map[int]subjectEpisodesResult)
		}
		ctx.episodesBySubject[subjectID] = subjectEpisodesResult{episodes: episodes, err: err}
	}
	return episodes, err
}

func groupMaxEpNumber(files []parsedLibraryFile, subject *BangumiSubjectInfo) int {
	maxEp := 0
	for _, file := range files {
		ep := 0
		if file.parsed.EpisodeNum > 0 {
			ep = file.parsed.EpisodeNum
		} else if subjectAllowsEpisodeOne(subject, file.parsed) {
			ep = 1
		}
		if ep > maxEp {
			maxEp = ep
		}
	}
	return maxEp
}

func (s *LibraryService) resolveCeiling(ctx *scanContext, subject *BangumiSubjectInfo, files []parsedLibraryFile) int {
	maxEp := groupMaxEpNumber(files, subject)
	if maxEp == 0 {
		return 0
	}
	base := 0
	if subject != nil {
		base = subject.TotalEpisodes
	}
	if maxEp <= base {
		return base
	}
	if subject == nil {
		log.Printf("[Library] 无法拉取剧集上限：条目为空")
		return 0
	}
	episodes, err := s.subjectEpisodes(ctx, subject.ID)
	if err != nil {
		log.Printf("[Library] 获取条目 %d 剧集失败，越界校验放行: %v", subject.ID, err)
		return 0
	}
	return episodeCeiling(subject, episodes)
}

func groupAllProducibleOverflow(files []parsedLibraryFile, subject *BangumiSubjectInfo, ceiling int) bool {
	if ceiling <= 0 {
		return false
	}
	saw := false
	for _, file := range files {
		ep := 0
		if file.parsed.EpisodeNum > 0 {
			ep = file.parsed.EpisodeNum
		} else if subjectAllowsEpisodeOne(subject, file.parsed) {
			ep = 1
		} else {
			continue
		}
		saw = true
		if ep <= ceiling {
			return false
		}
	}
	return saw
}

func boundTitleScore(title string, anime *models.Anime) float64 {
	norm := normalizeTitle(title)
	score := diceBigram(norm, normalizeTitle(anime.Title))
	if cn := diceBigram(norm, normalizeTitle(anime.TitleCn)); cn > score {
		score = cn
	}
	return score
}

func (s *LibraryService) processGroup(key groupKey, files []parsedLibraryFile, ctx *scanContext, result *LibraryScanResult) {
	if len(files) == 0 {
		return
	}
	groupDir := key.relDir
	title := key.title

	// 快通道：先于 bangumi==nil（离线可用）。
	boundAnimes, listErr := database.ListAnimesByFilePath(groupDir)
	if listErr != nil {
		log.Printf("[Library] 按目录查询番剧失败 %q: %v", groupDir, listErr)
	} else if len(boundAnimes) == 1 && boundAnimes[0].BangumiID > 0 {
		anime := boundAnimes[0]
		t := stripSeasonSuffix(title)
		if boundTitleScore(t, &anime) >= boundTitleMinScore {
			subject := &BangumiSubjectInfo{
				ID:            anime.BangumiID,
				Name:          anime.Title,
				NameCn:        anime.TitleCn,
				TotalEpisodes: anime.EpCount,
			}
			ceiling := s.resolveCeiling(ctx, subject, files)
			s.persistGroupEpisodes(&anime, files, subject, ceiling, result)
			return
		}
	}

	if s.bangumi == nil {
		addGroupUnidentified(result, files, "Bangumi 服务不可用")
		return
	}

	var searchResults []BangumiSearchItem
	var err error
	if year := groupASCIIYear(files); year != "" {
		searchResults, err = s.bangumi.Search(title + " " + year)
		if err != nil {
			addGroupUnidentified(result, files, "搜索失败: "+err.Error())
			return
		}
	}
	if len(searchResults) == 0 {
		searchResults, err = s.bangumi.Search(title)
		if err != nil {
			addGroupUnidentified(result, files, "搜索失败: "+err.Error())
			return
		}
	}

	// 渐进缩短搜索：完整标题无结果时，逐步去掉末尾单词重试
	if len(searchResults) == 0 {
		words := strings.Fields(title)
		for i := len(words) - 1; i > 0 && len(searchResults) == 0; i-- {
			shorter := strings.Join(words[:i], " ")
			searchResults, err = s.bangumi.Search(shorter)
			if err != nil {
				addGroupUnidentified(result, files, "搜索失败: "+err.Error())
				return
			}
		}
	}

	if len(searchResults) == 0 {
		addGroupUnidentified(result, files, "无搜索结果")
		return
	}

	decision := DecideBangumiMatch(title, searchResults)
	subjectCache := make(map[int]*BangumiSubjectInfo)

	// 组内任一 EpisodeNum==0 且第一轮未 Accept 才走别名轮。纯编号 TV 组零次额外 GetSubject。
	if (!decision.Accept || decision.Winner == nil) && groupHasNoEpisode(files) {
		aliasesByID := make(map[int][]string, len(decision.Candidates))
		for _, cand := range decision.Candidates {
			subject, getErr := s.bangumi.GetSubject(cand.ID)
			if getErr != nil {
				continue
			}
			subjectCache[cand.ID] = subject
			aliasesByID[cand.ID] = subject.Aliases
		}
		decision = DecideBangumiMatchWithAliases(title, searchResults, aliasesByID)
	}

	if !decision.Accept || decision.Winner == nil {
		candidates := decision.Candidates
		if candidates == nil {
			candidates = []MatchCandidate{}
		}
		for _, file := range files {
			result.Unidentified = append(result.Unidentified, UnidentifiedFile{
				FileName:   file.fileName,
				Reason:     "匹配置信度不足",
				FilePath:   file.relDir,
				Candidates: candidates,
			})
		}
		return
	}

	winner := decision.Winner
	subject := subjectCache[winner.ID]
	if subject == nil {
		subject, err = s.bangumi.GetSubject(winner.ID)
		if err != nil {
			log.Printf("[Library] 获取 Bangumi 详情失败 %d，使用搜索结果: %v", winner.ID, err)
			subject = &BangumiSubjectInfo{
				ID:            winner.ID,
				Name:          winner.Name,
				NameCn:        winner.NameCn,
				Summary:       winner.Summary,
				Cover:         winner.Cover,
				TotalEpisodes: winner.EpsCount,
			}
		}
	}

	anime, err := database.GetAnimeByBangumiID(subject.ID)
	if err != nil {
		addGroupUnidentified(result, files, "查询已有番剧失败")
		return
	}
	if anime != nil && anime.FilePath != groupDir {
		addGroupUnidentified(result, files, fmt.Sprintf("番剧已存在但目录不同（已有: %s，当前: %s）", anime.FilePath, groupDir))
		return
	}
	if anime != nil {
		ceiling := s.resolveCeiling(ctx, subject, files)
		s.persistGroupEpisodes(anime, files, subject, ceiling, result)
		return
	}

	ceiling := s.resolveCeiling(ctx, subject, files)
	if ceiling > 0 && groupMaxEpNumber(files, subject) > 0 && groupAllProducibleOverflow(files, subject, ceiling) {
		addGroupUnidentified(result, files, fmt.Sprintf("全部文件集数超出条目范围（上限 %d），疑似匹配错误", ceiling))
		return
	}
	if !groupProducesAnyEpisode(files, subject) {
		addGroupUnidentified(result, files, "无法识别集数")
		return
	}
	anime, err = database.CreateAnime(&models.Anime{
		Title:     subject.Name,
		TitleCn:   subject.NameCn,
		BangumiID: subject.ID,
		Cover:     subject.Cover,
		Summary:   subject.Summary,
		EpCount:   subject.TotalEpisodes,
		FilePath:  groupDir,
	})
	if err != nil {
		addGroupUnidentified(result, files, "创建番剧失败")
		return
	}
	if anime.FilePath != groupDir {
		addGroupUnidentified(result, files, fmt.Sprintf("番剧已存在但目录不同（已有: %s，当前: %s）", anime.FilePath, groupDir))
		return
	}
	result.NewAnimes++
	s.persistGroupEpisodes(anime, files, subject, ceiling, result)
}

func (s *LibraryService) persistGroupEpisodes(anime *models.Anime, files []parsedLibraryFile, subject *BangumiSubjectInfo, ceiling int, result *LibraryScanResult) {
	existingEpisodes, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		addGroupUnidentified(result, files, "查询已有集数失败")
		return
	}
	existingByNumber := make(map[int]models.Episode, len(existingEpisodes))
	for _, episode := range existingEpisodes {
		existingByNumber[episode.EpNumber] = episode
	}
	for _, file := range files {
		epNum := 0
		if file.parsed.EpisodeNum > 0 {
			epNum = file.parsed.EpisodeNum
		} else if subjectAllowsEpisodeOne(subject, file.parsed) {
			epNum = 1
		} else {
			addUnidentified(result, file.fileName, file.relDir, "无法识别集数")
			continue
		}
		if ceiling > 0 && epNum > ceiling {
			addUnidentified(result, file.fileName, file.relDir, fmt.Sprintf("集数超出条目范围（第 %d 集 / 上限 %d）", epNum, ceiling))
			continue
		}
		existing, exists := existingByNumber[epNum]
		if !exists {
			err := database.CreateEpisode(&models.Episode{
				AnimeID:  anime.ID,
				EpNumber: epNum,
				FilePath: file.fileName,
			})
			if err != nil {
				log.Printf("[Library] 创建集数失败 %q: %v", file.fileName, err)
				addUnidentified(result, file.fileName, file.relDir, "创建集数失败")
				continue
			}
			existingByNumber[epNum] = models.Episode{
				EpNumber: epNum,
				FilePath: file.fileName,
			}
			result.NewEpisodes++
			continue
		}
		// 集号已存在且文件名一致：正常跳过。
		if existing.FilePath == file.fileName {
			continue
		}
		// 集号已存在但文件名不一致：若旧文件仍在磁盘上，说明是同一集号的另一个文件，
		// 无法自动判定，报告给用户；若旧文件已不存在，则视为文件改名，更新 file_path。
		oldPath := filepath.Join(s.rootPath, anime.FilePath, existing.FilePath)
		if _, statErr := os.Stat(oldPath); statErr == nil {
			addUnidentified(result, file.fileName, file.relDir, "集数已存在（旧文件仍在磁盘上），无法自动关联")
			continue
		} else if !os.IsNotExist(statErr) {
			log.Printf("[Library] 检查旧文件 %q 失败: %v", oldPath, statErr)
			addUnidentified(result, file.fileName, file.relDir, "检查旧文件失败")
			continue
		}
		if err := database.UpdateEpisodeFilePath(existing.ID, file.fileName); err != nil {
			log.Printf("[Library] 更新改名文件路径失败 %q: %v", file.fileName, err)
			addUnidentified(result, file.fileName, file.relDir, "更新改名文件路径失败")
			continue
		}
		existingByNumber[epNum] = models.Episode{
			ID:       existing.ID,
			EpNumber: epNum,
			FilePath: file.fileName,
		}
		result.Skipped++
	}
}

func groupHasNoEpisode(files []parsedLibraryFile) bool {
	for _, file := range files {
		if file.parsed.EpisodeNum == 0 {
			return true
		}
	}
	return false
}

func groupASCIIYear(files []parsedLibraryFile) string {
	for _, file := range files {
		if year := filenameASCIIYear(file.fileName); year != "" {
			return year
		}
	}
	return ""
}

func filenameASCIIYear(filename string) string {
	for _, m := range bracketPattern.FindAllStringSubmatch(filename, -1) {
		if yearBracketContentRe.MatchString(m[1]) {
			return m[1]
		}
	}
	return ""
}

func groupProducesAnyEpisode(files []parsedLibraryFile, subject *BangumiSubjectInfo) bool {
	for _, file := range files {
		if file.parsed.EpisodeNum > 0 || subjectAllowsEpisodeOne(subject, file.parsed) {
			return true
		}
	}
	return false
}

func addGroupUnidentified(result *LibraryScanResult, files []parsedLibraryFile, reason string) {
	for _, file := range files {
		addUnidentified(result, file.fileName, file.relDir, reason)
	}
}

func addUnidentified(result *LibraryScanResult, fileName, relDir, reason string) {
	result.Unidentified = append(result.Unidentified, UnidentifiedFile{
		FileName:   fileName,
		Reason:     reason,
		FilePath:   relDir,
		Candidates: []MatchCandidate{},
	})
}
