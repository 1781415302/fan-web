package database

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// newAPIKeyTestDB 初始化独立的临时库并插入一个管理员用户作为密钥创建者，返回其 ID。
func newAPIKeyTestDB(t *testing.T) int64 {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "api-keys-test.db")
	if err := Init(databasePath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if DB != nil {
			_ = DB.Close()
			DB = nil
		}
	})
	if _, err := DB.Exec(`INSERT INTO users (username, password, is_admin) VALUES (?, ?, ?)`, "key-admin", "x", 1); err != nil {
		t.Fatal(err)
	}
	user, err := GetUserByUsername("key-admin")
	if err != nil {
		t.Fatal(err)
	}
	return user.ID
}

func countAPIKeys(t *testing.T) int {
	t.Helper()
	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM api_keys").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestCreateAPIKeyInsertsAndReturnsRow(t *testing.T) {
	userID := newAPIKeyTestDB(t)

	created, err := CreateAPIKey("hermes", "hash-1", "fw_abcdefgh...wxyz", userID)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID <= 0 {
		t.Fatalf("新行应带自增 id，got %#v", created)
	}
	if created.Name != "hermes" || created.KeyPrefix != "fw_abcdefgh...wxyz" || created.CreatedBy != userID {
		t.Fatalf("回查字段不匹配: %#v", created)
	}
	if created.LastUsedAt != nil {
		t.Fatalf("新建密钥 last_used_at 应为 nil，got %v", *created.LastUsedAt)
	}
	if created.CreatedAt.IsZero() {
		t.Fatalf("created_at 应由数据库默认值填充: %#v", created)
	}

	var storedHash string
	if err := DB.QueryRow("SELECT key_hash FROM api_keys WHERE id = ?", created.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash != "hash-1" {
		t.Fatalf("key_hash 应原样落库，got %q", storedHash)
	}
}

func TestCreateAPIKeyRejectsInvalidArguments(t *testing.T) {
	userID := newAPIKeyTestDB(t)

	cases := []struct {
		name      string
		keyName   string
		keyHash   string
		keyPrefix string
		createdBy int64
	}{
		{name: "空名称", keyName: "", keyHash: "h", keyPrefix: "p", createdBy: userID},
		{name: "空白名称", keyName: "  ", keyHash: "h", keyPrefix: "p", createdBy: userID},
		{name: "空哈希", keyName: "n", keyHash: "", keyPrefix: "p", createdBy: userID},
		{name: "空白哈希", keyName: "n", keyHash: "\t", keyPrefix: "p", createdBy: userID},
		{name: "空前缀", keyName: "n", keyHash: "h", keyPrefix: "", createdBy: userID},
		{name: "空白前缀", keyName: "n", keyHash: "h", keyPrefix: " ", createdBy: userID},
		{name: "创建者为 0", keyName: "n", keyHash: "h", keyPrefix: "p", createdBy: 0},
		{name: "创建者为负", keyName: "n", keyHash: "h", keyPrefix: "p", createdBy: -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CreateAPIKey(tc.keyName, tc.keyHash, tc.keyPrefix, tc.createdBy)
			if err == nil {
				t.Fatalf("期望参数错误，got %#v", got)
			}
			if got != nil {
				t.Fatalf("参数错误时不应返回记录，got %#v", got)
			}
		})
	}
	if n := countAPIKeys(t); n != 0 {
		t.Fatalf("参数非法时不得写入任何行，got %d", n)
	}
}

func TestCreateAPIKeyRejectsDuplicateHash(t *testing.T) {
	userID := newAPIKeyTestDB(t)

	if _, err := CreateAPIKey("first", "dup-hash", "fw_1", userID); err != nil {
		t.Fatal(err)
	}
	got, err := CreateAPIKey("second", "dup-hash", "fw_2", userID)
	if err == nil {
		t.Fatalf("重复 key_hash 应触发唯一约束错误，got %#v", got)
	}
	if got != nil {
		t.Fatalf("唯一约束失败时不应返回记录，got %#v", got)
	}
	if n := countAPIKeys(t); n != 1 {
		t.Fatalf("重复插入后应仍只有 1 行，got %d", n)
	}
}

func TestListAPIKeysEmptyAndDescending(t *testing.T) {
	userID := newAPIKeyTestDB(t)

	empty, err := ListAPIKeys()
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("无记录时应返回非 nil 空切片，got %#v", empty)
	}

	first, err := CreateAPIKey("a", "hash-a", "fw_a", userID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateAPIKey("b", "hash-b", "fw_b", userID)
	if err != nil {
		t.Fatal(err)
	}
	third, err := CreateAPIKey("c", "hash-c", "fw_c", userID)
	if err != nil {
		t.Fatal(err)
	}

	keys, err := ListAPIKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 {
		t.Fatalf("期望 3 条，got %#v", keys)
	}
	if keys[0].ID != third.ID || keys[1].ID != second.ID || keys[2].ID != first.ID {
		t.Fatalf("应按 id DESC 排序，got %d,%d,%d", keys[0].ID, keys[1].ID, keys[2].ID)
	}
	if keys[0].Name != "c" || keys[0].KeyPrefix != "fw_c" || keys[0].CreatedBy != userID || keys[0].LastUsedAt != nil {
		t.Fatalf("列表字段不匹配: %#v", keys[0])
	}
}

func TestGetAPIKeyByHash(t *testing.T) {
	userID := newAPIKeyTestDB(t)

	created, err := CreateAPIKey("hermes", "hash-x", "fw_x", userID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateAPIKey("other", "hash-y", "fw_y", userID); err != nil {
		t.Fatal(err)
	}

	got, err := GetAPIKeyByHash("hash-x")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || got.Name != "hermes" || got.CreatedBy != userID {
		t.Fatalf("按哈希应精确命中 hash-x，got %#v", got)
	}

	for _, miss := range []string{"missing", "", "HASH-X", "hash-"} {
		got, err := GetAPIKeyByHash(miss)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("哈希 %q 无行应返回 sql.ErrNoRows，got err=%v", miss, err)
		}
		if got != nil {
			t.Fatalf("哈希 %q 无行应返回 nil，got %#v", miss, got)
		}
	}
}

func TestTouchAPIKeyLastUsed(t *testing.T) {
	userID := newAPIKeyTestDB(t)

	created, err := CreateAPIKey("hermes", "hash-t", "fw_t", userID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := CreateAPIKey("other", "hash-o", "fw_o", userID)
	if err != nil {
		t.Fatal(err)
	}

	if err := TouchAPIKeyLastUsed(created.ID); err != nil {
		t.Fatal(err)
	}
	got, err := GetAPIKeyByHash("hash-t")
	if err != nil {
		t.Fatal(err)
	}
	if got.LastUsedAt == nil || got.LastUsedAt.IsZero() {
		t.Fatalf("Touch 后 last_used_at 应被写入，got %#v", got)
	}
	untouched, err := GetAPIKeyByHash("hash-o")
	if err != nil {
		t.Fatal(err)
	}
	if untouched.ID != other.ID || untouched.LastUsedAt != nil {
		t.Fatalf("未 Touch 的密钥 last_used_at 应保持 nil，got %#v", untouched)
	}

	if err := TouchAPIKeyLastUsed(999999); err != nil {
		t.Fatalf("影响 0 行应返回 nil，got %v", err)
	}
}

func TestDeleteAPIKey(t *testing.T) {
	userID := newAPIKeyTestDB(t)

	created, err := CreateAPIKey("hermes", "hash-d", "fw_d", userID)
	if err != nil {
		t.Fatal(err)
	}
	keep, err := CreateAPIKey("keep", "hash-k", "fw_k", userID)
	if err != nil {
		t.Fatal(err)
	}

	if err := DeleteAPIKey(created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := GetAPIKeyByHash("hash-d"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("删除后按哈希应查不到，got err=%v", err)
	}
	remaining, err := ListAPIKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ID != keep.ID {
		t.Fatalf("只应删除指定密钥，got %#v", remaining)
	}

	if err := DeleteAPIKey(created.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("重复删除（影响 0 行）应返回 sql.ErrNoRows，got %v", err)
	}
	if err := DeleteAPIKey(999999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("删除不存在 id 应返回 sql.ErrNoRows，got %v", err)
	}
}
