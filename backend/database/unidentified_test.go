package database

import (
	"math"
	"path/filepath"
	"testing"

	"fan-web/models"
)

func newUnidentifiedTestDB(t *testing.T) {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "unidentified-test.db")
	if err := Init(databasePath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if DB != nil {
			_ = DB.Close()
			DB = nil
		}
	})
}

func TestReplaceUnidentifiedOverwritesOnSecondCall(t *testing.T) {
	newUnidentifiedTestDB(t)

	first := []models.UnidentifiedFile{
		{
			FilePath: "Show",
			FileName: "01.mkv",
			Reason:   "匹配置信度不足",
			Candidates: []models.MatchCandidate{
				{ID: 123, Name: "Demo Show", NameCn: "演示番", Score: 0.72},
			},
		},
		{FilePath: "Other", FileName: "a.mkv", Reason: "无法解析"},
	}
	if err := ReplaceUnidentified(first); err != nil {
		t.Fatal(err)
	}
	items, total, err := ListUnidentified(1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("第一次写入期望 2 条，got total=%d len=%d", total, len(items))
	}

	second := []models.UnidentifiedFile{
		{FilePath: "Show", FileName: "02.mkv", Reason: "覆盖后"},
	}
	if err := ReplaceUnidentified(second); err != nil {
		t.Fatal(err)
	}
	items, total, err = ListUnidentified(1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("第二次覆盖期望 1 条，got total=%d len=%d items=%#v", total, len(items), items)
	}
	if items[0].FilePath != "Show" || items[0].FileName != "02.mkv" || items[0].Reason != "覆盖后" {
		t.Fatalf("覆盖后行不匹配: %#v", items[0])
	}
	if items[0].ID == 0 || items[0].UpdatedAt.IsZero() {
		t.Fatalf("GET 应带 id 与 updated_at: %#v", items[0])
	}
	if items[0].Candidates == nil {
		t.Fatal("candidates 不得为 nil")
	}

	if err := ReplaceUnidentified(nil); err != nil {
		t.Fatal(err)
	}
	_, total, err = ListUnidentified(1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatalf("空切片应清空表，got total=%d", total)
	}
}

func TestDeleteUnidentifiedByDirOnlyRemovesThatDirectory(t *testing.T) {
	newUnidentifiedTestDB(t)

	if err := ReplaceUnidentified([]models.UnidentifiedFile{
		{FilePath: "Show", FileName: "01.mkv", Reason: "r"},
		{FilePath: "Show", FileName: "02.mkv", Reason: "r"},
		{FilePath: "Other", FileName: "01.mkv", Reason: "r"},
		{FilePath: "", FileName: "root.mkv", Reason: "r"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteUnidentifiedByDir("Show"); err != nil {
		t.Fatal(err)
	}
	items, total, err := ListUnidentified(1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("期望只删 Show 目录，剩余 2 条，got %d items=%#v", total, items)
	}
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.FilePath+"/"+item.FileName] = true
	}
	if !seen["Other/01.mkv"] || !seen["/root.mkv"] {
		t.Fatalf("应保留 Other 与根目录文件，got %#v", items)
	}
	if seen["Show/01.mkv"] || seen["Show/02.mkv"] {
		t.Fatalf("Show 目录应已删除，got %#v", items)
	}
}

func TestListUnidentifiedClampsPageAndPageSize(t *testing.T) {
	newUnidentifiedTestDB(t)
	rows := make([]models.UnidentifiedFile, 0, 3)
	for _, name := range []string{"a.mkv", "b.mkv", "c.mkv"} {
		rows = append(rows, models.UnidentifiedFile{FilePath: "D", FileName: name, Reason: "r"})
	}
	if err := ReplaceUnidentified(rows); err != nil {
		t.Fatal(err)
	}

	items, total, err := ListUnidentified(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("page<1 且非法 pageSize 应按 1/50，got total=%d len=%d", total, len(items))
	}

	items, total, err = ListUnidentified(1, 200)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("pageSize 上限 100，got total=%d len=%d", total, len(items))
	}
}

func TestListUnidentifiedOverflowPageIsEmpty(t *testing.T) {
	newUnidentifiedTestDB(t)
	if err := ReplaceUnidentified([]models.UnidentifiedFile{
		{FilePath: "D", FileName: "a.mkv", Reason: "r"},
	}); err != nil {
		t.Fatal(err)
	}
	items, total, err := ListUnidentified(math.MaxInt, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 0 {
		t.Fatalf("overflow page should be empty with real total, got total=%d len=%d", total, len(items))
	}
}
