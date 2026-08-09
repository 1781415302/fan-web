package services

import (
	"os"
	"path/filepath"
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
