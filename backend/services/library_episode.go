package services

import (
	"math"
	"strings"
)

// libraryEpisodeNum 把解析结果规范成入库/单番扫描用的集号。
// Kind=="movie" 返回 1，否则返回 EpisodeNum。纯函数，Scan 与 Scanner 共用。
func libraryEpisodeNum(p ParsedFilename) int {
	if p.Kind == "movie" {
		return 1
	}
	return p.EpisodeNum
}

func subjectAllowsEpisodeOne(subject *BangumiSubjectInfo, parsed ParsedFilename) bool {
	if subject != nil && subject.TotalEpisodes == 1 {
		return true
	}
	if subject != nil && isMoviePlatform(subject.Platform) {
		return true
	}
	if parsed.Kind == "movie" && (subject == nil || subject.TotalEpisodes <= 1) {
		return true
	}
	return false
}

func isMoviePlatform(platform string) bool {
	if strings.Contains(platform, "剧场版") || strings.Contains(platform, "电影") {
		return true
	}
	return strings.Contains(strings.ToLower(platform), "movie")
}

// episodeCeiling 计算条目允许的最大集号。纯函数。
// 返回 max(subject.TotalEpisodes, ceil(episodes 中最大 Sort))；
// subject 可为 nil；两者都未知（<=0）时返回 0 = 无法判定（调用方放行）。
// episodes 为 nil/空 时只用 subject.TotalEpisodes。
func episodeCeiling(subject *BangumiSubjectInfo, episodes []BangumiEpisode) int {
	base := 0
	if subject != nil && subject.TotalEpisodes > 0 {
		base = subject.TotalEpisodes
	}
	if len(episodes) == 0 {
		return base
	}
	maxSort := episodes[0].Sort
	for _, ep := range episodes[1:] {
		if ep.Sort > maxSort {
			maxSort = ep.Sort
		}
	}
	sortCeil := int(math.Ceil(maxSort))
	if sortCeil > base {
		return sortCeil
	}
	return base
}
