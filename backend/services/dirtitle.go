package services

import (
	"path/filepath"
	"strings"
	"unicode"
)

// DirTitleHint 是目录链提取的标题候选。Title=="" 表示无有效候选。
type DirTitleHint struct {
	Title  string // 清洗后的标题候选（不含季号）
	Season int    // 目录链中识别到的季号，0=未知
}

const maxDirTitleDepth = 3

// genericDirNames 通用分类/容器目录黑名单（小写键，比较前分量对齐小写 trim 后精确匹配，
// 不做子串匹配——"2024年10月新番" 不等于 "新番"）。
var genericDirNames = map[string]bool{
	"anime":     true,
	"animes":    true,
	"animation": true,
	"新番":        true,
	"完结":        true,
	"完结番":       true,
	"连载":        true,
	"追番":        true,
	"番剧":        true,
	"动漫":        true,
	"动画":        true,
	"download":  true,
	"downloads": true,
	"下载":        true,
	"video":     true,
	"videos":    true,
	"视频":        true,
	"media":     true,
	"媒体":        true,
	"tv":        true,
	"movie":     true,
	"movies":    true,
	"电影":        true,
	"剧场版":       true,
	"bd":        true,
	"bdrip":     true,
	"web":       true,
	"webrip":    true,
	"待整理":       true,
	"未完成":       true,
}

// DeriveDirTitle 从相对目录路径提取标题候选。纯函数，无 IO。
// relDir 与 collectFiles 产出的 relDir 同格式（filepath 分隔符，""=根目录）。
func DeriveDirTitle(relDir string) DirTitleHint {
	if relDir == "" {
		return DirTitleHint{}
	}

	parts := strings.Split(relDir, string(filepath.Separator))
	var hint DirTitleHint
	depth := 0
	for i := len(parts) - 1; i >= 0 && depth < maxDirTitleDepth; i-- {
		comp := parts[i]
		if comp == "" || comp == "." {
			continue
		}
		depth++

		norm := normalizeFullwidthDigits(comp)
		if hint.Season == 0 {
			if s := extractSeason(norm); s > 0 {
				hint.Season = s
			}
		}

		if hint.Title == "" && dirComponentSurvives(norm) {
			hint.Title = cleanDirTitle(norm)
		}

		if hint.Title != "" && hint.Season > 0 {
			break
		}
	}
	return hint
}

// dirComponentSurvives 判定该层是否可作为标题候选（与契约顺序一致）。
func dirComponentSurvives(comp string) bool {
	if strings.TrimSpace(stripAll(comp, matchSeasonRe)) == "" {
		return false
	}
	if genericDirNames[strings.ToLower(strings.TrimSpace(comp))] {
		return false
	}
	if isMetadataBracket(comp) {
		return false
	}
	trimmed := strings.TrimSpace(comp)
	if yearBracketContentRe.MatchString(trimmed) || isAllDigits(trimmed) {
		return false
	}
	if strings.TrimSpace(stripDirBracketMetadata(comp)) == "" {
		return false
	}
	return true
}

func cleanDirTitle(comp string) string {
	title := stripDirBracketMetadata(comp)
	title = stripAll(title, matchSeasonRe)
	title = strings.Join(strings.Fields(title), " ")
	return strings.Trim(title, " -")
}

// stripDirBracketMetadata 与 parser 步骤 3 相同：元数据块删掉，其余保留内容。
func stripDirBracketMetadata(s string) string {
	return bracketPattern.ReplaceAllStringFunc(s, func(match string) string {
		content := match[1 : len(match)-1]
		if isMetadataBracket(content) {
			return " "
		}
		return " " + content + " "
	})
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
