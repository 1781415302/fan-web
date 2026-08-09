package services

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	githubOwner = "1781415302"
	githubRepo  = "fan-web"
	githubAPI   = "https://api.github.com/repos/" + githubOwner + "/" + githubRepo + "/releases/latest"
)

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Body    string        `json:"body"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type UpdateCheckResult struct {
	HasUpdate      bool   `json:"has_update"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	ReleaseNotes   string `json:"release_notes"`
	DownloadURL    string `json:"download_url,omitempty"`
	DownloadSize   int64  `json:"download_size,omitempty"`
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

func IsNewerVersion(current, latest string) bool {
	normalize := func(v string) []int {
		v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
		if v == "" || v == "dev" {
			return []int{0, 0, 0}
		}
		parts := strings.Split(v, ".")
		nums := make([]int, 0, len(parts))
		for _, p := range parts {
			p = strings.SplitN(p, "-", 2)[0]
			p = strings.SplitN(p, "+", 2)[0]
			n, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil {
				n = 0
			}
			nums = append(nums, n)
		}
		return nums
	}
	c := normalize(current)
	l := normalize(latest)
	maxLen := len(c)
	if len(l) > maxLen {
		maxLen = len(l)
	}
	for i := 0; i < maxLen; i++ {
		cv, lv := 0, 0
		if i < len(c) {
			cv = c[i]
		}
		if i < len(l) {
			lv = l[i]
		}
		if lv > cv {
			return true
		}
		if lv < cv {
			return false
		}
	}
	return false
}

func fetchLatestRelease() (*githubRelease, error) {
	req, err := http.NewRequest(http.MethodGet, githubAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "fan-web-updater")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接更新服务器: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("更新服务器返回异常: %d", resp.StatusCode)
	}
	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("解析更新信息失败: %w", err)
	}
	if release.TagName == "" {
		return nil, fmt.Errorf("更新信息异常: 缺少版本号")
	}
	return &release, nil
}

func findServerAsset(assets []githubAsset) *githubAsset {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	candidates := []string{
		fmt.Sprintf("fan-web-server-%s-%s", goos, goarch),
		fmt.Sprintf("fan-web-server-%s-%s.exe", goos, goarch),
	}
	for _, name := range candidates {
		for i := range assets {
			if assets[i].Name == name {
				return &assets[i]
			}
		}
	}
	return nil
}

func findSHA256Asset(assets []githubAsset) *githubAsset {
	for i := range assets {
		n := strings.ToLower(assets[i].Name)
		if n == "sha256sums.txt" || n == "sha256sums" || strings.HasPrefix(n, "sha256") {
			return &assets[i]
		}
	}
	return nil
}

func CheckUpdate(currentVersion string) (*UpdateCheckResult, error) {
	release, err := fetchLatestRelease()
	if err != nil {
		return nil, err
	}
	result := &UpdateCheckResult{
		CurrentVersion: currentVersion,
		LatestVersion:  release.TagName,
		ReleaseNotes:   release.Body,
		HasUpdate:      false,
	}
	// 服务器端与移动端共用同一个 Release 版本号。
	// 仅当本次发布包含当前平台对应的服务器二进制时才算作“有更新”，
	// 这样纯移动端发布不会触发服务器更新提示。
	if IsNewerVersion(currentVersion, release.TagName) {
		asset := findServerAsset(release.Assets)
		if asset != nil {
			result.HasUpdate = true
			result.DownloadURL = asset.BrowserDownloadURL
			result.DownloadSize = asset.Size
		}
	}
	return result, nil
}

func PerformUpdate(currentVersion string) error {
	release, err := fetchLatestRelease()
	if err != nil {
		return err
	}
	if !IsNewerVersion(currentVersion, release.TagName) {
		return fmt.Errorf("已是最新版本")
	}
	asset := findServerAsset(release.Assets)
	if asset == nil {
		return fmt.Errorf("未找到适用于当前平台(%s/%s)的更新包", runtime.GOOS, runtime.GOARCH)
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}

	if err := checkWritable(execPath); err != nil {
		return err
	}

	tmpPath := execPath + ".new"
	shaAsset := findSHA256Asset(release.Assets)

	if err := downloadFile(asset.BrowserDownloadURL, tmpPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("下载失败: %w", err)
	}

	if shaAsset != nil {
		if err := verifySHA256(tmpPath, asset.Name, shaAsset.BrowserDownloadURL); err != nil {
			os.Remove(tmpPath)
			return err
		}
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("设置权限失败: %w", err)
	}

	backupPath := execPath + ".old"
	os.Remove(backupPath)
	if err := os.Rename(execPath, backupPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("备份旧版本失败: %w", err)
	}
	if err := os.Rename(tmpPath, execPath); err != nil {
		_ = os.Rename(backupPath, execPath)
		os.Remove(tmpPath)
		return fmt.Errorf("替换二进制失败: %w", err)
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		p, _ := os.FindProcess(os.Getpid())
		if p != nil {
			_ = p.Signal(os.Interrupt)
			time.Sleep(800 * time.Millisecond)
			_ = p.Signal(os.Kill)
		}
		os.Exit(0)
	}()

	return nil
}

func checkWritable(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("无写入权限，请检查可执行文件权限或以有权限的用户运行")
		}
		return fmt.Errorf("文件不可写: %w", err)
	}
	f.Close()
	dir := path
	if idx := strings.LastIndex(path, string(os.PathSeparator)); idx >= 0 {
		dir = path[:idx]
		if dir == "" {
			dir = "."
		}
	}
	if err := checkDirWritable(dir); err != nil {
		return err
	}
	return nil
}

func checkDirWritable(dir string) error {
	tmp := dir + string(os.PathSeparator) + ".fan-web-write-test"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("无写入权限，请检查目录权限")
		}
		return fmt.Errorf("目录不可写: %w", err)
	}
	f.Close()
	os.Remove(tmp)
	return nil
}

func downloadFile(url, dest string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "fan-web-updater")
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败，状态码 %d", resp.StatusCode)
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return err
	}
	return nil
}

func verifySHA256(filePath, assetName, shaURL string) error {
	req, err := http.NewRequest(http.MethodGet, shaURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "fan-web-updater")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	expected := ""
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hash := fields[0]
		name := strings.TrimSpace(fields[1])
		name = strings.TrimPrefix(name, "*")
		if name == assetName {
			expected = strings.ToLower(hash)
			break
		}
	}
	if expected == "" {
		return nil
	}
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("校验失败: %w", err)
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("SHA256 校验失败，文件可能已损坏或被篡改")
	}
	return nil
}
