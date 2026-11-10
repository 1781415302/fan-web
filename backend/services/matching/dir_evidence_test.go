package matching

import (
	"path/filepath"
	"testing"
)

func dirTitleTexts(ev FileEvidence) []string {
	var out []string
	for _, t := range ev.Titles {
		out = append(out, t.Text)
	}
	return out
}

func assertOnlyDirTitle(t *testing.T, ev FileEvidence, want string) {
	t.Helper()
	if len(ev.Titles) != 1 || ev.Titles[0].Text != want || ev.Titles[0].Source != SourceDirectory {
		t.Fatalf("titles=%#v, want only directory title %q", ev.Titles, want)
	}
}

func TestDirSeasonSegmentsPureNumberFile(t *testing.T) {
	for _, dir := range []string{"Title/Season 2", "Title/S02", "Title/第二季", "Title/Season.02", "Title/2nd Season", "Title/第2期", "Title/ＳＥＡＳＯＮ ２"} {
		ev := ParseFile(FileInput{Name: "05.mkv", Dir: filepath.FromSlash(dir)})
		if ev.Ordinal.Season != 2 {
			t.Fatalf("%s: season=%d", dir, ev.Ordinal.Season)
		}
		if ev.Episode.First != 5 {
			t.Fatalf("%s: episode=%+v", dir, ev.Episode)
		}
		assertOnlyDirTitle(t, ev, "Title")
		if len(ev.Conflicts) != 0 {
			t.Fatalf("%s: conflicts=%v", dir, ev.Conflicts)
		}
	}
}

func TestDirSeasonSegmentsWithFileTitle(t *testing.T) {
	for _, dir := range []string{"Title/Season 2", "Title/S02", "Title/第二季"} {
		ev := ParseFile(FileInput{Name: "Title - 05.mkv", Dir: filepath.FromSlash(dir)})
		if ev.Ordinal.Season != 2 || ev.Episode.First != 5 {
			t.Fatalf("%s: season=%d ep=%+v", dir, ev.Ordinal.Season, ev.Episode)
		}
		if len(ev.Titles) != 1 || ev.Titles[0].Text != "Title" || ev.Titles[0].Source != SourceFile {
			t.Fatalf("%s: titles=%#v", dir, ev.Titles)
		}
	}
}

func TestDirSeasonConflictsWithFileSeason(t *testing.T) {
	ev := ParseFile(FileInput{Name: "Title S3 - 05.mkv", Dir: filepath.FromSlash("Title/Season 2")})
	if ev.Ordinal.Season != 3 {
		t.Fatalf("file season must win: %d", ev.Ordinal.Season)
	}
	if len(ev.Conflicts) == 0 {
		t.Fatalf("expected season conflict, got none")
	}
}

func TestDirOnlySeasonSegmentNoTitle(t *testing.T) {
	ev := ParseFile(FileInput{Name: "05.mkv", Dir: filepath.FromSlash("Season 2")})
	if len(ev.Titles) != 0 {
		t.Fatalf("season folder must not become title: %#v", ev.Titles)
	}
	if ev.Ordinal.Season != 0 {
		t.Fatalf("no directory title → season not applied: %d", ev.Ordinal.Season)
	}
}

func TestDirBatchNameCleaned(t *testing.T) {
	ev := ParseFile(FileInput{Name: "Title - 05.mkv", Dir: filepath.FromSlash("[Lilith-Raws] Title [Baha][WEB-DL][1080p AVC AAC][CHT]")})
	if ev.Episode.First != 5 {
		t.Fatalf("episode=%+v", ev.Episode)
	}
	ev2 := ParseFile(FileInput{Name: "05.mkv", Dir: filepath.FromSlash("[Lilith-Raws] Title [Baha][WEB-DL][1080p AVC AAC][CHT]")})
	assertOnlyDirTitle(t, ev2, "Title")
	if ev2.Episode.First != 5 {
		t.Fatalf("episode=%+v", ev2.Episode)
	}
	ev3 := ParseFile(FileInput{Name: "05.mkv", Dir: filepath.FromSlash("Anime/[Lilith-Raws] Title [Baha][WEB-DL][1080p AVC AAC][CHT]/Season 2")})
	assertOnlyDirTitle(t, ev3, "Title")
	if ev3.Ordinal.Season != 2 {
		t.Fatalf("season=%d", ev3.Ordinal.Season)
	}
}

func TestDirBatchNameKeepsYearAndRange(t *testing.T) {
	ev := ParseFile(FileInput{Name: "05.mkv", Dir: filepath.FromSlash("[Group] Title [2023][01-12][1080p]")})
	assertOnlyDirTitle(t, ev, "Title")
	if ev.Year != 0 {
		// 目录标题分支不应用目录年份（既有行为），只要求年份不进标题
		t.Logf("year=%d", ev.Year)
	}
	cleaned, season := preExtractDir(filepath.FromSlash("[Group] Title [2023][01-12][1080p]"))
	if cleaned != "Title (2023)" || season != 0 {
		t.Fatalf("cleaned=%q season=%d", cleaned, season)
	}
}

func TestDirGenericExtrasNotTitle(t *testing.T) {
	for _, sub := range []string{"SPs", "NCOP", "NCED", "Extras", "CDs", "Scans", "Sample"} {
		ev := ParseFile(FileInput{Name: "05.mkv", Dir: filepath.FromSlash("Title/" + sub)})
		assertOnlyDirTitle(t, ev, "Title")
	}
	ev := ParseFile(FileInput{Name: "NCOP.mkv", Dir: filepath.FromSlash("Title/SPs")})
	for _, ti := range ev.Titles {
		if ti.Source == SourceDirectory && ti.Text == "SPs" {
			t.Fatalf("SPs leaked as title: %#v", ev.Titles)
		}
	}
	ev2 := ParseFile(FileInput{Name: "05.mkv", Dir: filepath.FromSlash("Title/Season 2/SPs")})
	assertOnlyDirTitle(t, ev2, "Title")
	if ev2.Ordinal.Season != 2 {
		t.Fatalf("season=%d", ev2.Ordinal.Season)
	}
}

func TestPreExtractDirIdentityAndSeparator(t *testing.T) {
	for _, dir := range []string{"", "芙莉莲", "86", filepath.FromSlash("Anime/Title"), filepath.FromSlash("[K-ON!]")} {
		got, season := preExtractDir(dir)
		if got != dir || season != 0 {
			t.Fatalf("%q: got %q season=%d", dir, got, season)
		}
	}
	got, season := preExtractDir(filepath.FromSlash("A/Title/Season 2"))
	if got != filepath.FromSlash("A/Title") || season != 2 {
		t.Fatalf("got %q season=%d", got, season)
	}
	// 窗口外的祖先目录不因剔除而进入 parseDirectory 窗口
	got, _ = preExtractDir(filepath.FromSlash("Root/Collection/Title/Season 2/SPs"))
	if got != "Title" {
		t.Fatalf("got %q", got)
	}
}

func TestPreExtractDirSeasonConflictIsZero(t *testing.T) {
	_, season := preExtractDir(filepath.FromSlash("Title/Season 1/S02"))
	if season != 0 {
		t.Fatalf("conflicting season segments must yield 0, got %d", season)
	}
	_, season = preExtractDir(filepath.FromSlash("Title/Season 0"))
	if season != 0 {
		t.Fatalf("season 0 must yield 0, got %d", season)
	}
}

func TestApplyDirSeasonFallbackRules(t *testing.T) {
	ev := FileEvidence{Titles: []Title{{Text: "Title", Source: SourceDirectory}}}
	applyDirSeasonFallback(&ev, 2)
	if ev.Ordinal.Season != 2 {
		t.Fatalf("season=%d", ev.Ordinal.Season)
	}
	fileEv := FileEvidence{Titles: []Title{{Text: "Title", Source: SourceFile}}}
	applyDirSeasonFallback(&fileEv, 2)
	if fileEv.Ordinal.Season != 0 || len(fileEv.Conflicts) != 0 {
		t.Fatalf("file-title evidence must be untouched: %+v", fileEv)
	}
	none := FileEvidence{}
	applyDirSeasonFallback(&none, 2)
	if none.Ordinal.Season != 0 {
		t.Fatalf("no titles: season=%d", none.Ordinal.Season)
	}
	zero := FileEvidence{Titles: []Title{{Text: "Title", Source: SourceDirectory}}}
	applyDirSeasonFallback(&zero, 0)
	if zero.Ordinal.Season != 0 {
		t.Fatalf("dirSeason 0: season=%d", zero.Ordinal.Season)
	}
	diff := FileEvidence{Titles: []Title{{Text: "Title", Source: SourceDirectory}}}
	diff.Ordinal.Season = 3
	applyDirSeasonFallback(&diff, 2)
	applyDirSeasonFallback(&diff, 2)
	if diff.Ordinal.Season != 3 || len(diff.Conflicts) != 1 {
		t.Fatalf("differing season: %+v", diff)
	}
	applyDirSeasonFallback(nil, 2)
}

func TestDirExistingBehaviorKept(t *testing.T) {
	ev := ParseFile(FileInput{Name: "05.mkv", Dir: filepath.FromSlash("86")})
	assertOnlyDirTitle(t, ev, "86")
	ev2 := ParseFile(FileInput{Name: "05.mkv", Dir: filepath.FromSlash("芙莉莲")})
	assertOnlyDirTitle(t, ev2, "芙莉莲")
	ev3 := ParseFile(FileInput{Name: "05.mkv", Dir: filepath.FromSlash("[K-ON!]")})
	if got := dirTitleTexts(ev3); len(got) != 1 || got[0] != "[K-ON!]" {
		t.Fatalf("plain bracket title dir changed: %v", got)
	}
}
