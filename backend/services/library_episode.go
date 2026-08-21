package services

// libraryEpisodeNum 把解析结果规范成入库/单番扫描用的集号。
// Kind=="movie" 返回 1，否则返回 EpisodeNum。纯函数，Scan 与 Scanner 共用。
func libraryEpisodeNum(p ParsedFilename) int {
	if p.Kind == "movie" {
		return 1
	}
	return p.EpisodeNum
}
