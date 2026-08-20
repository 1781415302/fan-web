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

func (s *LibraryService) Scan() (*LibraryScanResult, error) {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	result := &LibraryScanResult{Unidentified: make([]UnidentifiedFile, 0)}
	allFiles, err := s.collectFiles()
	if err != nil {
		return nil, err
	}
	result.TotalFiles = len(allFiles)

	groups := make(map[string][]parsedLibraryFile)
	for _, file := range allFiles {
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

		parsed := ParseFilename(file.fileName)
		if parsed.Title == "" {
			addUnidentified(result, file.fileName, file.relDir, "无法解析文件名")
			continue
		}
		if parsed.EpisodeNum == 0 {
			addUnidentified(result, file.fileName, file.relDir, "无法识别集数")
			continue
		}
		groups[parsed.Title] = append(groups[parsed.Title], parsedLibraryFile{libraryFile: file, parsed: parsed})
	}

	titles := make([]string, 0, len(groups))
	for title := range groups {
		titles = append(titles, title)
	}
	sort.Strings(titles)
	for _, title := range titles {
		s.processGroup(title, groups[title], result)
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

func (s *LibraryService) processGroup(title string, files []parsedLibraryFile, result *LibraryScanResult) {
	if len(files) == 0 {
		return
	}
	groupDir := files[0].relDir
	for _, file := range files[1:] {
		if file.relDir != groupDir {
			addGroupUnidentified(result, files, "同一标题的文件位于不同目录")
			return
		}
	}

	if s.bangumi == nil {
		addGroupUnidentified(result, files, "Bangumi 服务不可用")
		return
	}
	searchResults, err := s.bangumi.Search(title)
	if err != nil {
		addGroupUnidentified(result, files, "搜索失败: "+err.Error())
		return
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
	subject, err := s.bangumi.GetSubject(winner.ID)
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

	anime, err := database.GetAnimeByBangumiID(subject.ID)
	if err != nil {
		addGroupUnidentified(result, files, "查询已有番剧失败")
		return
	}
	if anime != nil && anime.FilePath != groupDir {
		addGroupUnidentified(result, files, fmt.Sprintf("番剧已存在但目录不同（已有: %s，当前: %s）", anime.FilePath, groupDir))
		return
	}
	if anime == nil {
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
	}

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
		existing, exists := existingByNumber[file.parsed.EpisodeNum]
		if !exists {
			err := database.CreateEpisode(&models.Episode{
				AnimeID:  anime.ID,
				EpNumber: file.parsed.EpisodeNum,
				FilePath: file.fileName,
			})
			if err != nil {
				log.Printf("[Library] 创建集数失败 %q: %v", file.fileName, err)
				addUnidentified(result, file.fileName, file.relDir, "创建集数失败")
				continue
			}
			existingByNumber[file.parsed.EpisodeNum] = models.Episode{
				EpNumber: file.parsed.EpisodeNum,
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
		existingByNumber[file.parsed.EpisodeNum] = models.Episode{
			ID:       existing.ID,
			EpNumber: file.parsed.EpisodeNum,
			FilePath: file.fileName,
		}
		result.Skipped++
	}
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
