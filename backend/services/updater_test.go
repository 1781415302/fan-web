package services

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsNewerVersion(t *testing.T) {
	cases := []struct {
		current string
		latest  string
		want    bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.0", "v1.1.0", true},
		{"v1.0.0", "v2.0.0", true},
		{"v1.1.0", "v1.1.0", false},
		{"v1.2.0", "v1.1.0", false},
		{"v1.10.0", "v1.9.0", false},
		{"v1.9.0", "v1.10.0", true},
		{"v1.0.0", "v1.0.0", false},
		{"dev", "v1.0.0", true},
		{"v1.0.0", "dev", false},
		{"1.0.0", "1.0.1", true},
		{"v1.0", "v1.0.1", true},
	}
	for _, c := range cases {
		got := IsNewerVersion(c.current, c.latest)
		if got != c.want {
			t.Errorf("IsNewerVersion(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestCheckWritableOnRunningExecutable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fan-web-server")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// 曾有的回归：checkWritable 用 os.OpenFile(path, O_WRONLY) 检测写权限，
	// 对"运行中的可执行文件"会触发 Linux 的 ETXTBSY（text file busy），
	// 导致自更新永远报"文件不可写"。现在只检查目录可写，运行中的二进制也应通过。
	if err := checkWritable(path); err != nil {
		t.Fatalf("checkWritable(%q) on a running-style executable dir should succeed, got: %v", path, err)
	}

	// 目录不可写时应报错（真正限制替换的是目录权限，因为替换走 os.Rename）。
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	if err := checkWritable(path); err == nil {
		t.Fatal("checkWritable should fail when directory is not writable")
	}
}

func writeTestBinary(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "server.bin")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVerifySHA256(t *testing.T) {
	assetName := "fan-web-server-linux-amd64"
	content := []byte("fan-web test binary content")
	sum := sha256.Sum256(content)
	gotHash := hex.EncodeToString(sum[:])

	cases := []struct {
		name       string
		checksum   string
		status     int
		assetName  string
		wantErr    bool
		errKeyword string
	}{
		{
			name:      "correct hash passes",
			checksum:  fmt.Sprintf("%s  %s\n", gotHash, assetName),
			status:    http.StatusOK,
			assetName: assetName,
		},
		{
			name:       "hash mismatch fails",
			checksum:   fmt.Sprintf("%s  %s\n", strings.Repeat("0", 64), assetName),
			status:     http.StatusOK,
			assetName:  assetName,
			wantErr:    true,
			errKeyword: "SHA256",
		},
		{
			name:       "missing target record fails",
			checksum:   fmt.Sprintf("%s  some-other-file\n", gotHash),
			status:     http.StatusOK,
			assetName:  assetName,
			wantErr:    true,
			errKeyword: "未找到",
		},
		{
			name:       "illegal hash length fails",
			checksum:   fmt.Sprintf("%s  %s\n", strings.Repeat("a", 32), assetName),
			status:     http.StatusOK,
			assetName:  assetName,
			wantErr:    true,
			errKeyword: "非法",
		},
		{
			name:       "non-hex hash fails",
			checksum:   fmt.Sprintf("%s  %s\n", strings.Repeat("z", 64), assetName),
			status:     http.StatusOK,
			assetName:  assetName,
			wantErr:    true,
			errKeyword: "非法",
		},
		{
			name:       "http 404 fails",
			checksum:   "",
			status:     http.StatusNotFound,
			assetName:  assetName,
			wantErr:    true,
			errKeyword: "状态码",
		},
		{
			name:       "sha256sum binary star suffix passes",
			checksum:   fmt.Sprintf("%s *%s\n", gotHash, assetName),
			status:     http.StatusOK,
			assetName:  assetName,
			errKeyword: "",
		},
	}

	for i := range cases {
		tc := cases[i]
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status != http.StatusOK {
					w.WriteHeader(tc.status)
					return
				}
				_, _ = w.Write([]byte(tc.checksum))
			}))
			defer server.Close()

			filePath := writeTestBinary(t, content)
			err := verifySHA256(filePath, tc.assetName, server.URL+"/SHA256SUMS.txt")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errKeyword != "" && !strings.Contains(err.Error(), tc.errKeyword) {
					t.Fatalf("expected error containing %q, got: %v", tc.errKeyword, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected success, got: %v", err)
			}
		})
	}
}

func TestVerifySHA256ConnectionFailureFailsClosed(t *testing.T) {
	// 用一个不会被监听的地址模拟连接失败，校验必须失败而非放行。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	filePath := writeTestBinary(t, []byte("content"))
	err := verifySHA256(filePath, "fan-web-server-linux-amd64", url+"/SHA256SUMS.txt")
	if err == nil {
		t.Fatal("连接失败时 verifySHA256 必须返回错误（失败关闭）")
	}
}

// TestReplaceExecutablePreservesOldBackup 是自更新回滚安全的核心回归：
// 替换成功后必须保留 .old 回滚副本，不能在此时删除（旧实现替换后立即 os.Remove
// 会导致新版起不来时无回滚）。.old 改由新版本启动后 CleanupUpdateBackup 清理。
func TestReplaceExecutablePreservesOldBackup(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "fan-web-server")
	tmpPath := execPath + ".new"
	if err := os.WriteFile(execPath, []byte("current"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmpPath, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := replaceExecutable(execPath, tmpPath); err != nil {
		t.Fatalf("replaceExecutable 失败: %v", err)
	}

	// 新版本已就位。
	if data, err := os.ReadFile(execPath); err != nil || string(data) != "new" {
		t.Fatalf("期望 execPath 为新二进制，got data=%q err=%v", data, err)
	}
	// .old 回滚副本必须保留（本次修复的核心）。
	if _, err := os.Lstat(execPath + ".old"); err != nil {
		t.Fatalf("期望 .old 回滚副本保留，got err=%v", err)
	}
	// tmpPath 已被 rename 消费。
	if _, err := os.Lstat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("期望 tmpPath 已移走，got err=%v", err)
	}
	// .old 内容应为旧版本。
	if data, err := os.ReadFile(execPath + ".old"); err != nil || string(data) != "current" {
		t.Fatalf("期望 .old 为旧二进制，got data=%q err=%v", data, err)
	}
}

// TestReplaceExecutableFailsWhenBackupExists 验证残留 .old 时拒绝覆盖，保护唯一回滚副本。
func TestReplaceExecutableFailsWhenBackupExists(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "fan-web-server")
	tmpPath := execPath + ".new"
	if err := os.WriteFile(execPath, []byte("current"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmpPath, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(execPath+".old", []byte("older"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := replaceExecutable(execPath, tmpPath)
	if !errors.Is(err, errUpdateBackupExists) {
		t.Fatalf("期望 errUpdateBackupExists，got %v", err)
	}
	// 现场不被破坏：当前二进制与残留 .old 都还在，tmpPath 被清理。
	if data, _ := os.ReadFile(execPath); string(data) != "current" {
		t.Fatal("当前二进制不应被改动")
	}
	if data, _ := os.ReadFile(execPath + ".old"); string(data) != "older" {
		t.Fatal("残留 .old 不应被改动")
	}
	if _, err := os.Lstat(tmpPath); !os.IsNotExist(err) {
		t.Fatal("tmpPath 应被清理")
	}
}

// TestReplaceExecutableRollsBackOnFailure 验证第二步 rename 失败时回滚到旧版本。
// 用一个不存在的 tmpPath 触发 os.Rename(tmpPath, execPath) 失败。
func TestReplaceExecutableRollsBackOnFailure(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "fan-web-server")
	// tmpPath 不存在 -> 第一步 backup 成功，第二步 rename tmpPath->execPath 失败。
	tmpPath := execPath + ".new"
	if err := os.WriteFile(execPath, []byte("current"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := replaceExecutable(execPath, tmpPath)
	if err == nil {
		t.Fatal("期望替换失败")
	}
	// 旧版本应被回滚恢复。
	if data, _ := os.ReadFile(execPath); string(data) != "current" {
		t.Fatalf("期望回滚后 execPath 仍为旧二进制，got %q", data)
	}
}

func TestCleanupUpdateBackupRemovesStaleOld(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "fan-web-server")
	if err := os.WriteFile(execPath, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(execPath+".old", []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	cleanupUpdateBackupAt(execPath)

	if _, err := os.Lstat(execPath + ".old"); !os.IsNotExist(err) {
		t.Fatalf("期望 .old 已晋升消失，got err=%v", err)
	}
	if data, err := os.ReadFile(execPath + ".prev"); err != nil || string(data) != "old" {
		t.Fatalf("期望 .prev 为旧二进制，got data=%q err=%v", data, err)
	}
	// 当前可执行文件不受影响。
	if _, err := os.Lstat(execPath); err != nil {
		t.Fatalf("可执行文件不应被误删: %v", err)
	}
}

func TestCleanupUpdateBackupNoOpWithoutOld(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "fan-web-server")
	if err := os.WriteFile(execPath, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 无 .old，应无副作用。
	cleanupUpdateBackupAt(execPath)
	if _, err := os.Lstat(execPath); err != nil {
		t.Fatalf("可执行文件不应被误删: %v", err)
	}
}

func TestCleanupUpdateBackupLeavesPrevWhenNoOld(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "fan-web-server")
	if err := os.WriteFile(execPath, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(execPath+".prev", []byte("prev-bytes"), 0o755); err != nil {
		t.Fatal(err)
	}

	cleanupUpdateBackupAt(execPath)

	if data, err := os.ReadFile(execPath + ".prev"); err != nil || string(data) != "prev-bytes" {
		t.Fatalf("无 .old 时 .prev 字节应不变，got data=%q err=%v", data, err)
	}
	if _, err := os.Lstat(execPath + ".old"); !os.IsNotExist(err) {
		t.Fatalf("不应凭空造出 .old，got err=%v", err)
	}
}

func TestCleanupUpdateBackupPromotesOldOverPrev(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "fan-web-server")
	if err := os.WriteFile(execPath, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(execPath+".old", []byte("old-bytes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(execPath+".prev", []byte("prev-bytes"), 0o755); err != nil {
		t.Fatal(err)
	}

	cleanupUpdateBackupAt(execPath)

	if _, err := os.Lstat(execPath + ".old"); !os.IsNotExist(err) {
		t.Fatalf("期望 .old 已晋升消失，got err=%v", err)
	}
	if data, err := os.ReadFile(execPath + ".prev"); err != nil || string(data) != "old-bytes" {
		t.Fatalf("期望 .prev 为原 .old 字节，got data=%q err=%v", data, err)
	}
}

func TestReplaceExecutableFailsWhenOldAndPrevExist(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "fan-web-server")
	tmpPath := execPath + ".new"
	if err := os.WriteFile(execPath, []byte("current"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmpPath, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(execPath+".old", []byte("older"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(execPath+".prev", []byte("prev-bytes"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := replaceExecutable(execPath, tmpPath)
	if !errors.Is(err, errUpdateBackupExists) {
		t.Fatalf("期望 errUpdateBackupExists，got %v", err)
	}
	if data, _ := os.ReadFile(execPath); string(data) != "current" {
		t.Fatal("当前二进制不应被改动")
	}
	if data, _ := os.ReadFile(execPath + ".old"); string(data) != "older" {
		t.Fatal("残留 .old 不应被改动")
	}
	if data, _ := os.ReadFile(execPath + ".prev"); string(data) != "prev-bytes" {
		t.Fatal("残留 .prev 不应被改动")
	}
	if _, err := os.Lstat(tmpPath); !os.IsNotExist(err) {
		t.Fatal("tmpPath 应被清理")
	}
}

func TestReplaceExecutableDeletesPrevThenBacksUp(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "fan-web-server")
	tmpPath := execPath + ".new"
	if err := os.WriteFile(execPath, []byte("current"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmpPath, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(execPath+".prev", []byte("prev-bytes"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := replaceExecutable(execPath, tmpPath); err != nil {
		t.Fatalf("replaceExecutable 失败: %v", err)
	}

	if data, err := os.ReadFile(execPath); err != nil || string(data) != "new" {
		t.Fatalf("期望 execPath 为新二进制，got data=%q err=%v", data, err)
	}
	if data, err := os.ReadFile(execPath + ".old"); err != nil || string(data) != "current" {
		t.Fatalf("期望 .old 为旧二进制，got data=%q err=%v", data, err)
	}
	if _, err := os.Lstat(execPath + ".prev"); !os.IsNotExist(err) {
		t.Fatalf("期望 .prev 已删除，got err=%v", err)
	}
	if _, err := os.Lstat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("期望 tmpPath 已移走，got err=%v", err)
	}
}

func TestRejectStaleUpdateBackupBeforeDownload(t *testing.T) {
	execPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	oldPath := execPath + ".old"
	if err := os.WriteFile(oldPath, []byte("stale-old"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(oldPath)
	})

	if !HasStaleUpdateBackup() {
		t.Fatal("expected HasStaleUpdateBackup after creating .old")
	}
	err = rejectStaleUpdateBackup()
	if !errors.Is(err, errUpdateBackupExists) {
		t.Fatalf("expected errUpdateBackupExists before download, got %v", err)
	}
	if !strings.Contains(err.Error(), "检测到更新残留备份") {
		t.Fatalf("expected user-facing stale backup message, got %v", err)
	}

	if err := os.Remove(oldPath); err != nil {
		t.Fatal(err)
	}
	if err := rejectStaleUpdateBackup(); err != nil {
		t.Fatalf("no .old must allow update to proceed to download, got %v", err)
	}
}
