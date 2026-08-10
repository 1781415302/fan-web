package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveWrites0600AndLoadsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := Default()
	cfg.Configured = true
	cfg.Admin.Username = "alice"
	cfg.Video.RootPath = "/video"

	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions, got %v", info.Mode().Perm())
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Configured {
		t.Fatal("expected Configured=true after loading saved config")
	}
	if loaded.Admin.Username != "alice" || loaded.Video.RootPath != "/video" {
		t.Fatalf("unexpected loaded values: %+v", loaded)
	}
}

func TestSaveMissingParentDirFailsAndNoTempLeft(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "missing", "sub")
	path := filepath.Join(dir, "config.yaml")
	cfg := Default()
	if err := cfg.Save(path); err == nil {
		t.Fatal("expected save to fail for missing parent dir")
	}
	// parent 下不应留下任何临时文件（missing 目录本身不存在）。
	entries, listErr := os.ReadDir(parent)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no leftover temp files in parent, got %v", entries)
	}
}

func TestSaveYAMLDoesNotContainConfiguredField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := Default()
	cfg.Configured = true
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "configured") {
		t.Fatalf("saved YAML must not contain configured field:\n%s", data)
	}
	// 可完整解析。
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("saved YAML must parse: %v", err)
	}
	_ = loaded
}

func TestPreflightSaveWritableDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := Default()
	if err := cfg.PreflightSave(path); err != nil {
		t.Fatalf("expected preflight to pass for writable dir, got %v", err)
	}
}

func TestPreflightSaveMissingDirFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "config.yaml")
	cfg := Default()
	if err := cfg.PreflightSave(path); err == nil {
		t.Fatal("expected preflight to fail for missing parent dir")
	}
}

func TestSaveUsesUniqueTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	legacyTempFiles := []string{
		filepath.Join(dir, ".config.yaml.preflight"),
		filepath.Join(dir, ".config.yaml.tmp"),
	}
	for _, legacyPath := range legacyTempFiles {
		if err := os.WriteFile(legacyPath, []byte("sentinel"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cfg := Default()
	if err := cfg.PreflightSave(path); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}

	for _, legacyPath := range legacyTempFiles {
		data, err := os.ReadFile(legacyPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "sentinel" {
			t.Fatalf("temporary save must not overwrite %s", legacyPath)
		}
	}
}

func TestSaveMinimalConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := Default()
	cfg.Server.Port = 9001
	cfg.JWT.Expire = 24 * time.Hour
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.Port != 9001 || loaded.JWT.Expire != 24*time.Hour {
		t.Fatalf("unexpected round-trip values: %+v", loaded)
	}
}
