package services

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"fan-web/models"
)

var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".mov": true,
	".flv": true, ".webm": true, ".ts": true, ".m4v": true,
}

var epPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\[(\d{1,3})\]`),
	regexp.MustCompile(`-\s*(\d{1,3})\b`),
	regexp.MustCompile(`(?i)ep\.?\s*(\d{1,3})\b`),
	regexp.MustCompile(`第\s*(\d{1,3})\s*[集話话]`),
	regexp.MustCompile(`(?i)S\d{1,2}E(\d{1,3})\b`),
	regexp.MustCompile(`(?i)(\d{1,3})\.(?:mkv|mp4|avi|mov|flv|webm|ts|m4v)$`),
}

var ErrInvalidVideoPath = errors.New("文件目录必须是视频根目录下的相对目录")

type ScannerService struct {
	rootPath string
}

func NewScannerService(rootPath string) *ScannerService {
	return &ScannerService{rootPath: rootPath}
}

// SetRootPath 更新视频根目录，初始化完成时调用。
func (s *ScannerService) SetRootPath(rootPath string) {
	s.rootPath = rootPath
}

// RootPath 返回当前视频根目录。
func (s *ScannerService) RootPath() string {
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

	seen := make(map[int]bool)
	episodes := make([]models.Episode, 0)
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

		epNumber := extractEpisodeNumber(name)
		if epNumber <= 0 || seen[epNumber] {
			continue
		}
		seen[epNumber] = true
		episodes = append(episodes, models.Episode{EpNumber: epNumber, FilePath: name})
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
	return resolved, nil
}

func (s *ScannerService) resolveDirectory(dirPath string) (string, error) {
	if err := ValidateRelativeVideoPath(dirPath); err != nil {
		return "", err
	}

	root, err := filepath.Abs(s.rootPath)
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
