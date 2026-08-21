package services

import "strings"

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
