package services

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
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
	StaleOld       bool   `json:"stale_old"`
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// performUpdateMu 串行化自更新全过程（下载+校验+替换+重启）。
// 并发触发会互相截断下载文件、覆盖 .old 回滚备份，最坏情况下把替换后已上线的
// 二进制写坏。CheckUpdate 只读远端信息，不需要此锁。
var performUpdateMu sync.Mutex

// errUpdateBackupExists 表示替换前检测到残留的 .old 回滚副本，拒绝覆盖以免丢失回滚。
var errUpdateBackupExists = errors.New("update backup already exists")

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

// staleOldAt 报告 execPath+".old" 是否存在（残留回滚闸）。
func staleOldAt(execPath string) bool {
	_, err := os.Lstat(execPath + ".old")
	return err == nil
}

// HasStaleUpdateBackup 探测当前可执行文件旁是否残留 .old。
// CheckUpdate 成功路径与 Check 在 GitHub 失败时的手写 gin.H 共用，禁止写死 false。
func HasStaleUpdateBackup() bool {
	execPath, err := os.Executable()
	if err != nil {
		return false
	}
	return staleOldAt(execPath)
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
		StaleOld:       HasStaleUpdateBackup(),
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
	// 用 TryLock 而非 Lock：已在更新时第二个请求直接失败返回，
	// 而不是阻塞到进程被重启（阻塞中的 HTTP 请求会随进程退出而连接断开）。
	if !performUpdateMu.TryLock() {
		return fmt.Errorf("更新已在进行中，请勿重复触发")
	}
	defer performUpdateMu.Unlock()

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
	if runtime.GOOS == "windows" {
		// Windows 下正在运行的可执行文件被加载器独占锁定（未以 FILE_SHARE_DELETE
		// 方式打开），os.Rename 无法将其改名备份，替换必然失败。直接返回明确提示
		// 让用户手动替换，避免无谓的完整下载与校验耗时。
		return fmt.Errorf("Windows 平台暂不支持自动更新，请到 GitHub Releases 手动下载 %s 并替换可执行文件", asset.Name)
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
	if shaAsset == nil {
		return fmt.Errorf("发布缺少 SHA256SUMS.txt，已取消更新")
	}

	// 清理上次中断可能残留的下载文件（downloadFile 用 O_EXCL 创建，目标已存在会失败）。
	os.Remove(tmpPath)
	if err := downloadFile(asset.BrowserDownloadURL, tmpPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("下载失败: %w", err)
	}

	// 校验二进制大小与 GitHub 资产声明一致。
	if asset.Size > 0 {
		info, err := os.Stat(tmpPath)
		if err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("读取下载文件大小失败: %w", err)
		}
		if info.Size() != asset.Size {
			os.Remove(tmpPath)
			return fmt.Errorf("下载文件大小不匹配（期望 %d 字节，实际 %d 字节），已取消更新", asset.Size, info.Size())
		}
	}

	if err := verifySHA256(tmpPath, asset.Name, shaAsset.BrowserDownloadURL); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("设置权限失败: %w", err)
	}

	backupPath, err := replaceExecutable(execPath, tmpPath)
	if err != nil {
		if errors.Is(err, errUpdateBackupExists) {
			return fmt.Errorf("检测到更新残留备份 %s，请确认上一次更新已成功启动（新版本会在启动后自动清理该文件）后手动删除再重试", backupPath)
		}
		return err
	}
	// .old 回滚副本在此刻意保留：新版尚未启动，删除它会丧失回滚能力。
	// 由新版本成功启动（绑定端口）后通过 CleanupUpdateBackup 清理。

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

// replaceExecutable 用 tmpPath（已下载校验的新二进制）替换 execPath：先把当前二进制
// 改名为 .old 作为回滚备份，再把新二进制改名为 execPath。
//
// 关键：替换成功后保留 .old，不在此处删除——由新版本成功启动后通过 CleanupUpdateBackup
// 清理。这样若新版因数据库迁移失败、配置错误等起不来，旧版二进制仍在，可手动回滚。
// 替换前若已存在 .old（上一次更新的新版本未成功启动、未自动清理），拒绝覆盖以免丢失
// 唯一回滚副本。任一步失败均清理 tmpPath 并尝试回滚到旧版本，保证现场不被写坏。
func replaceExecutable(execPath, tmpPath string) (string, error) {
	backupPath := execPath + ".old"
	if _, err := os.Lstat(backupPath); err == nil {
		os.Remove(tmpPath)
		return backupPath, errUpdateBackupExists
	}
	// .prev 是已成功交接的上一版，不挡更新；造新 .old 之前先删掉，避免残留两份旧二进制。
	prevPath := execPath + ".prev"
	if _, err := os.Lstat(prevPath); err == nil {
		if err := os.Remove(prevPath); err != nil {
			os.Remove(tmpPath)
			return backupPath, fmt.Errorf("清理上一版本备份失败: %w", err)
		}
	}
	if err := os.Rename(execPath, backupPath); err != nil {
		os.Remove(tmpPath)
		return backupPath, fmt.Errorf("备份旧版本失败: %w", err)
	}
	if err := os.Rename(tmpPath, execPath); err != nil {
		_ = os.Rename(backupPath, execPath) // 回滚到旧版本
		os.Remove(tmpPath)
		return backupPath, fmt.Errorf("替换二进制失败: %w", err)
	}
	return backupPath, nil
}

// CleanupUpdateBackup 将自更新留下的上一版本回滚副本（<可执行文件>.old）
// 晋升为 .prev。由新版本在成功启动（绑定端口）后调用：只有新版本确认能正常
// 运行时才交接回滚闸。成功路径把 .old rename 为 .prev（已有 .prev 则先删除），
// 不 os.Remove(.old)。无 .old 时不碰已有 .prev。失败仅记录日志，不影响启动。
func CleanupUpdateBackup() {
	execPath, err := os.Executable()
	if err != nil {
		return
	}
	cleanupUpdateBackupAt(execPath)
}

// cleanupUpdateBackupAt 将 execPath 同目录下的 .old 晋升为 .prev；无 .old 则无操作。
// 拆出便于测试（不依赖 os.Executable）。
func cleanupUpdateBackupAt(execPath string) {
	oldPath := execPath + ".old"
	prevPath := execPath + ".prev"
	if _, err := os.Lstat(oldPath); err != nil {
		return
	}
	if _, err := os.Lstat(prevPath); err == nil {
		if err := os.Remove(prevPath); err != nil {
			log.Printf("清理上一版本备份 %s 失败: %v（可手动删除）", prevPath, err)
			return
		}
	}
	if err := os.Rename(oldPath, prevPath); err != nil {
		log.Printf("晋升上一版本备份 %s → %s 失败: %v（可手动处理）", oldPath, prevPath, err)
		return
	}
	log.Printf("已将上一版本备份 %s 晋升为 %s", oldPath, prevPath)
}

func checkWritable(path string) error {
	// 注意：不要用 os.OpenFile(path, O_WRONLY) 去测可写性。
	// 目标是"正在运行的可执行文件"时，Linux 会返回 ETXTBSY（text file busy），
	// 导致自更新永远误报失败。实际替换走 os.Rename，只依赖目录写权限，
	// 因此这里只检查目录是否可写即可。
	dir := path
	if idx := strings.LastIndex(path, string(os.PathSeparator)); idx >= 0 {
		dir = path[:idx]
		if dir == "" {
			dir = "."
		}
	}
	return checkDirWritable(dir)
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
	// O_EXCL：目标文件已存在（并发请求或残留文件）时直接失败，
	// 防止两个请求用 O_TRUNC 互相截断同一个下载文件。
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return err
	}
	return nil
}

// maxChecksumBytes 限制 checksum 响应大小，防止异常响应无界占用内存。
const maxChecksumBytes = 1 << 20 // 1 MiB

func verifySHA256(filePath, assetName, shaURL string) error {
	req, err := http.NewRequest(http.MethodGet, shaURL, nil)
	if err != nil {
		return fmt.Errorf("创建校验请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "fan-web-updater")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载校验文件失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载校验文件失败，状态码 %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, maxChecksumBytes)
	expected := ""
	scanner := bufio.NewScanner(limited)
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
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("解析校验文件失败: %w", err)
	}
	if expected == "" {
		return fmt.Errorf("校验文件中未找到 %s 的摘要，已取消更新", assetName)
	}
	if len(expected) != 64 || !isHex(expected) {
		return fmt.Errorf("校验摘要格式非法，已取消更新")
	}

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开下载文件校验失败: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("计算校验值失败: %w", err)
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("SHA256 校验失败，文件可能已损坏或被篡改")
	}
	return nil
}

func isHex(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}
