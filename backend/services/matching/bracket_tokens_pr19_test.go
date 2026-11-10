package matching

import "testing"

// PR #19 审核问题 3：预抽取到互斥的显式季号必须进入 Conflicts（硬冲突），不能只写诊断 Notes。
func TestParseFileConflictingBracketSeasonsIsHardConflict(t *testing.T) {
	ev := ParseFile(FileInput{Name: "[Group] Confirm Show [S02E01][S03E01].mkv", Dir: "Confirm Show"})
	if len(ev.Conflicts) == 0 {
		t.Fatalf("互斥季号应为硬冲突，got season=%d ep=%+v conflicts=%v", ev.Ordinal.Season, ev.Episode, ev.Conflicts)
	}
	// 同季同集重复标注不是冲突
	same := ParseFile(FileInput{Name: "[Group] Confirm Show [S02E01][S02E01].mkv", Dir: "Confirm Show"})
	if len(same.Conflicts) != 0 || same.Ordinal.Season != 2 {
		t.Fatalf("重复同季标注不应冲突: season=%d conflicts=%v", same.Ordinal.Season, same.Conflicts)
	}
}
