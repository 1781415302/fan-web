package services

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

type ParsedFilename struct {
	Title      string
	EpisodeNum int
	Season     int // 0=unknown
	FileName   string
	Kind       string `json:"-"` // 只能是 "episode" 或 "movie"；禁止空串
}

// 匹配所有 [...] 方括号块
var bracketPattern = regexp.MustCompile(`\[([^\]]*)\]`)

// 匹配开头的方括号块（通常是字幕组名）
var firstBracketPattern = regexp.MustCompile(`^\s*\[[^\]]*\]\s*`)

// 元数据关键词列表，出现在方括号内说明是编码/分辨率/语言等标签而非标题
var metadataKeywords = []string{
	"1080p", "720p", "480p", "2160p", "4k",
	"hevc", "avc", "h264", "h265", "h.264", "h.265", "x264", "x265",
	"10bit", "8bit", "10-bit", "8-bit",
	"aac", "flac", "mp3", "opus", "ac3",
	"webrip", "webdl", "web-dl", "bdrip", "bluray",
	"srt", "ass", "ssa",
	"jpsc", "gbsc", "cr", "bilibili", "crunchyroll",
	"简繁", "简体", "繁体", "内封", "内嵌", "外挂", "中字",
	"big5", "chs", "cht",
}

var latinMovieTokenRe = regexp.MustCompile(`(?i)(?:^|[^A-Za-z])(?:movie|film)(?:[^A-Za-z]|$)`)
var yearBracketContentRe = regexp.MustCompile(`^(?:19|20)\d{2}$`)
var weakTitleRe = regexp.MustCompile(`(?i)^v\d+$`)
var movieDenyTokenRe = regexp.MustCompile(`(?i)(?:^|[^A-Za-z])(?:NCOP|NCED|SPECIAL|Trailer|Preview|Menu|OVA|OAD|OP|ED|PV|CM|SP)(?:[^A-Za-z]|$)`)
var standaloneE02Re = regexp.MustCompile(`(?i)(?:^|[^A-Za-z])E\d{2}(?:[^A-Za-z]|$)`)
var zerosBracketRe = regexp.MustCompile(`^0+$`)

func ParseFilename(filename string) ParsedFilename {
	parsed := ParsedFilename{
		FileName:   filename,
		EpisodeNum: extractEpisodeNumber(filename),
	}

	// 1. 去除扩展名，并把全角数字规范成 ASCII，便于抽集数/剥标题。
	title := strings.TrimSuffix(filename, filepath.Ext(filename))
	title = normalizeFullwidthDigits(title)

	// 2. 去除开头的方括号块（字幕组名）
	title = firstBracketPattern.ReplaceAllString(title, "")

	// 3. 逐个处理剩余方括号：元数据删除，标题保留内容
	title = bracketPattern.ReplaceAllStringFunc(title, func(match string) string {
		content := match[1 : len(match)-1] // 去掉首尾的 [ ]
		if isMetadataBracket(content) {
			return " "
		}
		return " " + content + " "
	})

	// 4. 在剥离集数之前提取季号，避免 S01E05 一类模式把季信息带走。
	parsed.Season = extractSeason(title)

	// 5. 去除非方括号内的集数模式（如 - 01、EP03、S01E05）
	for _, pattern := range epPatterns {
		title = pattern.ReplaceAllString(title, " ")
	}

	// 6. 清理：合并空格、去除首尾的空格和横线
	title = strings.Join(strings.Fields(title), " ")
	title = strings.Trim(title, " -")
	if parsed.Season > 0 {
		title = stripAll(title, matchSeasonRe)
		title = strings.Join(strings.Fields(title), " ")
		title = strings.Trim(title, " -")
		title = fmt.Sprintf("%s 第%d季", title, parsed.Season)
	}

	parsed.Title = title
	parsed.Kind = filenameKind(parsed)
	return parsed
}

// filenameKind 在 Title/EpisodeNum/Season 填好之后判定。默认 episode。
// Kind=movie 仅当无集号且文件名含电影标记词；年份不是电影门闩。
func filenameKind(p ParsedFilename) string {
	if p.EpisodeNum > 0 {
		return "episode"
	}
	if weakTitleRe.MatchString(p.Title) {
		return "episode"
	}
	if movieDenied(p) {
		return "episode"
	}
	if hasMovieToken(p.FileName) && !movieOrFilmSoleTitleToken(p.Title) {
		return "movie"
	}
	return "episode"
}

func normalizeFullwidthDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '０' && r <= '９' {
			return r - '０' + '0'
		}
		return r
	}, s)
}

func hasMovieToken(filename string) bool {
	if strings.Contains(filename, "剧场版") || strings.Contains(filename, "劇場版") {
		return true
	}
	return latinMovieTokenRe.MatchString(filename)
}

func isStrongTitle(title string) bool {
	if len(strings.Fields(title)) >= 2 {
		return true
	}
	han := 0
	for _, r := range title {
		if unicode.Is(unicode.Han, r) {
			han++
			if han >= 2 {
				return true
			}
		}
	}
	return false
}

func movieOrFilmSoleTitleToken(title string) bool {
	tokens := strings.Fields(title)
	if len(tokens) != 1 {
		return false
	}
	lower := strings.ToLower(tokens[0])
	return lower == "movie" || lower == "film"
}

func movieDenied(p ParsedFilename) bool {
	if p.Season > 0 {
		return true
	}
	if hasDenyToken(p.Title) {
		return true
	}
	if standaloneE02Re.MatchString(p.Title) {
		return true
	}
	for _, m := range bracketPattern.FindAllStringSubmatch(p.FileName, -1) {
		content := m[1]
		if hasDenyToken(content) || zerosBracketRe.MatchString(content) || standaloneE02Re.MatchString(content) {
			return true
		}
	}
	return false
}

func hasDenyToken(s string) bool {
	if strings.Contains(s, "特别篇") {
		return true
	}
	return movieDenyTokenRe.MatchString(s)
}

// isMetadataBracket 判断方括号内容是否为元数据（编码、分辨率、集数、语言标签等）
func isMetadataBracket(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" {
		return true
	}

	// 纯数字或带修订号的集数编号（01、01v2）
	matched, _ := regexp.MatchString(`(?i)^\d{1,4}(?:v\d+)?$`, lower)
	if matched {
		return true
	}

	// [E01] / [e01] / [E01v2] 是集数元数据，不得漏进 Title
	matched, _ = regexp.MatchString(`(?i)^e\d{1,3}(?:v\d+)?$`, lower)
	if matched {
		return true
	}

	// 包含元数据关键词（含语言/封装标签，如 简繁、中字、内封 等）
	for _, kw := range metadataKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}

	// 不再按"全 CJK"一刀切删除：中文字幕组命名常把中文标题放进方括号
	//（如 [千夏字幕组][葬送的芙莉莲][第01话][1080p]），全 CJK 判断会把标题误当元数据删除。
	return false
}
