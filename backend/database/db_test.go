package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInitPragmasApplyToEveryConnection(t *testing.T) {
	paths := []string{
		filepath.Join(t.TempDir(), "t.db"),
		filepath.Join(t.TempDir(), "with space", "t.db"),
	}
	for _, dbPath := range paths {
		dbPath := dbPath
		t.Run(dbPath, func(t *testing.T) {
			if err := Init(dbPath); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if DB != nil {
					_ = DB.Close()
					DB = nil
				}
			})

			// 强制连接池在下次 Query 时新建连接，验证 DSN 级 PRAGMA 对每条连接生效。
			DB.SetMaxIdleConns(0)

			for i := 0; i < 2; i++ {
				var foreignKeys, busyTimeout int
				var journalMode string
				if err := DB.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
					t.Fatalf("round %d foreign_keys: %v", i, err)
				}
				if err := DB.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
					t.Fatalf("round %d busy_timeout: %v", i, err)
				}
				if err := DB.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
					t.Fatalf("round %d journal_mode: %v", i, err)
				}
				if foreignKeys != 1 {
					t.Fatalf("round %d foreign_keys = %d, want 1", i, foreignKeys)
				}
				if busyTimeout != 5000 {
					t.Fatalf("round %d busy_timeout = %d, want 5000", i, busyTimeout)
				}
				if journalMode != "wal" {
					t.Fatalf("round %d journal_mode = %q, want %q", i, journalMode, "wal")
				}
			}
		})
	}
}

func TestSQLiteDSNRejectsQuestionMark(t *testing.T) {
	_, err := sqliteDSN("/tmp/evil?.db")
	if err == nil {
		t.Fatal("expected error for path containing '?'")
	}
	if !strings.Contains(err.Error(), "?") {
		t.Fatalf("error should mention '?': %v", err)
	}

	dsn, err := sqliteDSN("/tmp/ok.db")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dsn, "/tmp/ok.db?") {
		t.Fatalf("dsn = %q, want prefix /tmp/ok.db?", dsn)
	}
	if !strings.Contains(dsn, sqliteConnPragmas) {
		t.Fatalf("dsn missing pragmas: %q", dsn)
	}
}
