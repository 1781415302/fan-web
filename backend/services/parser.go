package services

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

type ParsedFilename struct {
	Title      string
	EpisodeNum int
	FileName   string
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
	"简繁", "简体", "繁体", "内封", "内嵌", "外挂",
	"big5", "chs", "cht",
}

func ParseFilename(filename string) ParsedFilename {
	parsed := ParsedFilename{
		FileName:   filename,
		EpisodeNum: extractEpisodeNumber(filename),
	}

	// 1. 去除扩展名
	title := strings.TrimSuffix(filename, filepath.Ext(filename))

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

	// 4. 去除非方括号内的集数模式（如 - 01、EP03、S01E05）
	for _, pattern := range epPatterns {
		title = pattern.ReplaceAllString(title, " ")
	}

	// 5. 清理：合并空格、去除首尾的空格和横线
	title = strings.Join(strings.Fields(title), " ")
	title = strings.Trim(title, " -")

	parsed.Title = title
	return parsed
}

// isMetadataBracket 判断方括号内容是否为元数据（编码、分辨率、集数、语言标签等）
func isMetadataBracket(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" {
		return true
	}

	// 纯数字（集数编号）
	matched, _ := regexp.MatchString(`^\d{1,4}$`, lower)
	if matched {
		return true
	}

	// 包含元数据关键词
	for _, kw := range metadataKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}

	// 全是 CJK 字符（语言标签如 简繁内封）
	allCJK := true
	for _, r := range lower {
		if !unicode.Is(unicode.Han, r) {
			allCJK = false
			break
		}
	}
	if allCJK {
		return true
	}

	return false
}
