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

func TestDeleteUnidentifiedFilesOnlyListedNames(t *testing.T) {
	newUnidentifiedTestDB(t)

	if err := ReplaceUnidentified([]models.UnidentifiedFile{
		{FilePath: "Show", FileName: "01.mkv", Reason: "r"},
		{FilePath: "Show", FileName: "02.mkv", Reason: "r"},
		{FilePath: "Show", FileName: "03.mkv", Reason: "r"},
		{FilePath: "Other", FileName: "01.mkv", Reason: "r"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteUnidentifiedFiles("Show", []string{"01.mkv", "03.mkv"}); err != nil {
		t.Fatal(err)
	}
	items, total, err := ListUnidentified(1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("want 2 remaining, got %d items=%#v", total, items)
	}
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.FilePath+"/"+item.FileName] = true
	}
	if !seen["Show/02.mkv"] || !seen["Other/01.mkv"] {
		t.Fatalf("should keep Show/02 and Other/01, got %#v", items)
	}
	if seen["Show/01.mkv"] || seen["Show/03.mkv"] {
		t.Fatalf("listed names should be deleted, got %#v", items)
	}
}

func TestDeleteUnidentifiedFilesEmptyNamesNoop(t *testing.T) {
	newUnidentifiedTestDB(t)
	if err := ReplaceUnidentified([]models.UnidentifiedFile{
		{FilePath: "Show", FileName: "01.mkv", Reason: "r"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteUnidentifiedFiles("Show", nil); err != nil {
		t.Fatal(err)
	}
	if err := DeleteUnidentifiedFiles("Show", []string{}); err != nil {
		t.Fatal(err)
	}
	_, total, err := ListUnidentified(1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("empty names must be noop, got total=%d", total)
	}
}

func TestDeleteUnidentifiedFilesRejectsEmptyName(t *testing.T) {
	newUnidentifiedTestDB(t)
	if err := DeleteUnidentifiedFiles("Show", []string{"01.mkv", " "}); err == nil {
		t.Fatal("expected error for blank file name")
	}
}

func countUnidentifiedRows(t *testing.T) int {
	t.Helper()
	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM unidentified_files").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func unidentifiedByKey(t *testing.T) map[string]models.UnidentifiedFile {
	t.Helper()
	items, _, err := ListUnidentified(1, 100)
	if err != nil {
		t.Fatal(err)
	}
	byKey := make(map[string]models.UnidentifiedFile, len(items))
	for _, item := range items {
		byKey[item.FilePath+"|"+item.FileName] = item
	}
	return byKey
}

func TestUpsertUnidentifiedFilesEmptyIsNoop(t *testing.T) {
	newUnidentifiedTestDB(t)
	if err := ReplaceUnidentified([]models.UnidentifiedFile{
		{FilePath: "Show", FileName: "01.mkv", Reason: "r"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := UpsertUnidentifiedFiles(nil); err != nil {
		t.Fatal(err)
	}
	if err := UpsertUnidentifiedFiles([]models.UnidentifiedFile{}); err != nil {
		t.Fatal(err)
	}
	byKey := unidentifiedByKey(t)
	if len(byKey) != 1 || byKey["Show|01.mkv"].Reason != "r" {
		t.Fatalf("空切片应为零操作，got %#v", byKey)
	}
}

func TestUpsertUnidentifiedFilesInsertsAndUpdates(t *testing.T) {
	newUnidentifiedTestDB(t)
	if err := ReplaceUnidentified([]models.UnidentifiedFile{
		{FilePath: "Show", FileName: "01.mkv", Reason: "旧原因", Candidates: []models.MatchCandidate{{ID: 1, Name: "Old"}}},
		{FilePath: "Other", FileName: "keep.mkv", Reason: "保留"},
	}); err != nil {
		t.Fatal(err)
	}
	before := unidentifiedByKey(t)
	existing := before["Show|01.mkv"]
	keep := before["Other|keep.mkv"]
	// 把既有行的 updated_at 拨回过去，便于断言冲突更新会刷新它（CURRENT_TIMESTAMP 精度为秒）。
	if _, err := DB.Exec(
		"UPDATE unidentified_files SET updated_at = '2000-01-01 00:00:00' WHERE id IN (?, ?)",
		existing.ID, keep.ID,
	); err != nil {
		t.Fatal(err)
	}

	if err := UpsertUnidentifiedFiles([]models.UnidentifiedFile{
		{
			FilePath: "Show",
			FileName: "01.mkv",
			Reason:   "新原因",
			Candidates: []models.MatchCandidate{
				{ID: 123, Name: "Fate", NameCn: "命运", Score: 0.66},
				{ID: 456, Name: "Fate/Zero", Score: 0.61},
			},
		},
		{FilePath: "Show", FileName: "02.mkv", Reason: "新增"},
	}); err != nil {
		t.Fatal(err)
	}

	after := unidentifiedByKey(t)
	if len(after) != 3 {
		t.Fatalf("期望 1 条更新 + 1 条新增 + 1 条保留，共 3 条，got %#v", after)
	}
	updated := after["Show|01.mkv"]
	if updated.ID != existing.ID {
		t.Fatalf("冲突更新应保留原行 id %d，got %d", existing.ID, updated.ID)
	}
	if updated.Reason != "新原因" {
		t.Fatalf("reason 应被更新，got %q", updated.Reason)
	}
	if len(updated.Candidates) != 2 || updated.Candidates[0].ID != 123 || updated.Candidates[0].NameCn != "命运" ||
		updated.Candidates[0].Score != 0.66 || updated.Candidates[1].ID != 456 {
		t.Fatalf("candidates 应被整体替换，got %#v", updated.Candidates)
	}
	if updated.UpdatedAt.Year() == 2000 {
		t.Fatalf("冲突更新应刷新 updated_at，got %v", updated.UpdatedAt)
	}

	inserted, ok := after["Show|02.mkv"]
	if !ok || inserted.Reason != "新增" || inserted.ID == 0 {
		t.Fatalf("新文件应被插入，got %#v", inserted)
	}
	if inserted.Candidates == nil || len(inserted.Candidates) != 0 {
		t.Fatalf("nil candidates 应按既有序列化写为空数组，got %#v", inserted.Candidates)
	}
	var rawCandidates string
	if err := DB.QueryRow("SELECT candidates FROM unidentified_files WHERE id = ?", inserted.ID).Scan(&rawCandidates); err != nil {
		t.Fatal(err)
	}
	if rawCandidates != "[]" {
		t.Fatalf("nil candidates 应落库为 \"[]\"，got %q", rawCandidates)
	}

	untouched := after["Other|keep.mkv"]
	if untouched.ID != keep.ID || untouched.Reason != "保留" || untouched.UpdatedAt.Year() != 2000 {
		t.Fatalf("未在入参中的条目不得被改动，got %#v", untouched)
	}
}

func TestUpsertUnidentifiedFilesNormalizesFilePath(t *testing.T) {
	newUnidentifiedTestDB(t)

	if err := UpsertUnidentifiedFiles([]models.UnidentifiedFile{
		{FilePath: `Show\S1`, FileName: "01.mkv", Reason: "第一次"},
	}); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := DB.QueryRow("SELECT file_path FROM unidentified_files WHERE file_name = ?", "01.mkv").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "Show/S1" {
		t.Fatalf("file_path 应归一化为 /，got %q", stored)
	}

	// 以 / 形式再写同一文件，应命中同一唯一键走更新而非新增。
	if err := UpsertUnidentifiedFiles([]models.UnidentifiedFile{
		{FilePath: "Show/S1", FileName: "01.mkv", Reason: "第二次"},
	}); err != nil {
		t.Fatal(err)
	}
	if n := countUnidentifiedRows(t); n != 1 {
		t.Fatalf("不同分隔符写同一文件应只保留 1 行，got %d", n)
	}
	byKey := unidentifiedByKey(t)
	if byKey["Show/S1|01.mkv"].Reason != "第二次" {
		t.Fatalf("应更新为第二次，got %#v", byKey)
	}
}

func TestUpsertUnidentifiedFilesRejectsBlankFileName(t *testing.T) {
	newUnidentifiedTestDB(t)

	for _, blank := range []string{"", "  ", "\t"} {
		err := UpsertUnidentifiedFiles([]models.UnidentifiedFile{
			{FilePath: "Show", FileName: "01.mkv", Reason: "合法"},
			{FilePath: "Show", FileName: blank, Reason: "非法"},
		})
		if err == nil {
			t.Fatalf("文件名 %q 应返回参数错误", blank)
		}
	}
	if n := countUnidentifiedRows(t); n != 0 {
		t.Fatalf("参数错误时不得写入任何行（含同批合法行），got %d", n)
	}
}

func TestDeleteUnidentifiedByDirNormalizesSeparators(t *testing.T) {
	newUnidentifiedTestDB(t)

	// ReplaceUnidentified 原样落库，借此构造历史遗留的 \ 与 / 混存数据。
	if err := ReplaceUnidentified([]models.UnidentifiedFile{
		{FilePath: `win\dir`, FileName: "01.mkv", Reason: "r"},
		{FilePath: "unix/dir", FileName: "01.mkv", Reason: "r"},
		{FilePath: "win", FileName: "parent.mkv", Reason: "r"},
		{FilePath: `win\dir\sub`, FileName: "child.mkv", Reason: "r"},
	}); err != nil {
		t.Fatal(err)
	}

	// 库里存 a\b，传 a/b。
	if err := DeleteUnidentifiedByDir("win/dir"); err != nil {
		t.Fatal(err)
	}
	// 库里存 a/b，传 a\b。
	if err := DeleteUnidentifiedByDir(`unix\dir`); err != nil {
		t.Fatal(err)
	}

	byKey := unidentifiedByKey(t)
	if _, ok := byKey[`win\dir|01.mkv`]; ok {
		t.Fatalf("存 \\ 传 / 应删除，got %#v", byKey)
	}
	if _, ok := byKey["unix/dir|01.mkv"]; ok {
		t.Fatalf("存 / 传 \\ 应删除，got %#v", byKey)
	}
	if len(byKey) != 2 {
		t.Fatalf("父目录与子目录条目应保留，got %#v", byKey)
	}
	if _, ok := byKey["win|parent.mkv"]; !ok {
		t.Fatalf("父目录条目应保留，got %#v", byKey)
	}
	if _, ok := byKey[`win\dir\sub|child.mkv`]; !ok {
		t.Fatalf("子目录条目应保留，got %#v", byKey)
	}
}

func TestDeleteUnidentifiedFilesNormalizesSeparators(t *testing.T) {
	newUnidentifiedTestDB(t)

	if err := ReplaceUnidentified([]models.UnidentifiedFile{
		{FilePath: `win\dir`, FileName: "01.mkv", Reason: "r"},
		{FilePath: `win\dir`, FileName: "02.mkv", Reason: "r"},
		{FilePath: "unix/dir", FileName: "01.mkv", Reason: "r"},
		{FilePath: "unix/dir", FileName: "02.mkv", Reason: "r"},
	}); err != nil {
		t.Fatal(err)
	}

	// 库里存 a\b，传 a/b。
	if err := DeleteUnidentifiedFiles("win/dir", []string{"01.mkv"}); err != nil {
		t.Fatal(err)
	}
	// 库里存 a/b，传 a\b。
	if err := DeleteUnidentifiedFiles(`unix\dir`, []string{"02.mkv"}); err != nil {
		t.Fatal(err)
	}

	byKey := unidentifiedByKey(t)
	if len(byKey) != 2 {
		t.Fatalf("期望各删 1 条剩 2 条，got %#v", byKey)
	}
	if _, ok := byKey[`win\dir|02.mkv`]; !ok {
		t.Fatalf("未列出的 win\\dir/02.mkv 应保留，got %#v", byKey)
	}
	if _, ok := byKey["unix/dir|01.mkv"]; !ok {
		t.Fatalf("未列出的 unix/dir/01.mkv 应保留，got %#v", byKey)
	}
}
