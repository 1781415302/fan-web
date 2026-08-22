package services

import "testing"

func TestEpisodeCeiling(t *testing.T) {
	subject12 := &BangumiSubjectInfo{TotalEpisodes: 12}
	subject1 := &BangumiSubjectInfo{TotalEpisodes: 1}
	subject0 := &BangumiSubjectInfo{TotalEpisodes: 0}
	eps13 := []BangumiEpisode{{Sort: 13}}
	eps12p5 := []BangumiEpisode{{Sort: 12.5}}

	cases := []struct {
		name     string
		subject  *BangumiSubjectInfo
		episodes []BangumiEpisode
		want     int
	}{
		{"12 plus maxSort 13", subject12, eps13, 13},
		{"1 plus nil episodes", subject1, nil, 1},
		{"0 plus empty", subject0, []BangumiEpisode{}, 0},
		{"nil plus Sort 12.5", nil, eps12p5, 13},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := episodeCeiling(tc.subject, tc.episodes)
			if got != tc.want {
				t.Fatalf("episodeCeiling = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestGroupMaxEpNumber(t *testing.T) {
	tv := &BangumiSubjectInfo{TotalEpisodes: 12}
	single := &BangumiSubjectInfo{TotalEpisodes: 1}

	got := groupMaxEpNumber([]parsedLibraryFile{
		{parsed: ParsedFilename{EpisodeNum: 3}},
		{parsed: ParsedFilename{EpisodeNum: 5}},
	}, tv)
	if got != 5 {
		t.Fatalf("numbered max = %d, want 5", got)
	}

	got = groupMaxEpNumber([]parsedLibraryFile{
		{parsed: ParsedFilename{EpisodeNum: 0, Title: "Movie"}},
	}, single)
	if got != 1 {
		t.Fatalf("allows ep1 = %d, want 1", got)
	}

	got = groupMaxEpNumber([]parsedLibraryFile{
		{parsed: ParsedFilename{EpisodeNum: 0, Title: "Show"}},
	}, tv)
	if got != 0 {
		t.Fatalf("no producible = %d, want 0", got)
	}
}
