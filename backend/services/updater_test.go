package services

import (
	"crypto/sha256"
	"encoding/hex"
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
