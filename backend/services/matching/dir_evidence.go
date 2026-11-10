package matching

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// dirWindowDepth 与 parseDirectory 一致：只看最内层 3 个非空目录段。
const dirWindowDepth = 3

var (
	// 纯季目录段：Season 2 / Season.02 / S02 / S2 / 2nd Season / 第二季 / 第2期。
	reDirSeasonEn  = regexp.MustCompile(`(?i)^(?:season|s)[\s._\-]*(\d{1,2})$`)
	reDirSeasonOrd = regexp.MustCompile(`(?i)^(\d{1,2})(?:st|nd|rd|th)[\s._\-]*season$`)
	reDirSeasonCN  = regexp.MustCompile(`^第\s*([0-9一二三四五六七八九十百]+)\s*[季期]$`)
	// 批量目录名中的集数区间块，如 [01-12]、[01~24 Fin]。
	reDirRangeBlock = regexp.MustCompile(`(?i)^\d{1,4}\s*[-~～]\s*\d{1,4}(?:\s*(?:fin|end|完))?$`)
)

// extraGenericDirNames 附属内容目录段（特典/无字幕 OP·ED/CD/扫图/样片），
// 仅不当标题；是否过滤出库不在本期（见 4.6）。与 genericDirNames 一样按小写比较。
var extraGenericDirNames = map[string]bool{
	"sps": true, "ncop": true, "ncops": true, "nced": true, "nceds": true,
	"extra": true, "extras": true, "cd": true, "cds": true,
	"scan": true, "scans": true, "sample": true, "samples": true,
}

// preExtractDir 清洗目录串并识别目录季号（契约 4.0 第 3 条）。
//   - 纯季目录段（Season N / S0N / 第N季）：记录季号并剔除该段；
//   - 附属内容 generic 段（SPs/NCOP/NCED/Extras/CDs/Scans/Sample）：剔除；
//   - 批量目录名（[组] Title [Baha][WEB-DL][规格]）：只保留标题部分。
//
// 只处理 parseDirectory 会读取的最内层 3 个非空段；无任何改动时原样返回 dir。
// 有改动时返回窗口内清洗后的段，以 filepath.Separator 连接（OS 分隔符保持），
// 不让更外层的祖先目录因剔除而进入 parseDirectory 的窗口。
// 多个纯季段季号不一致时季号为 0（保守，不猜）。
func preExtractDir(dir string) (cleaned string, season int) {
	if dir == "" {
		return dir, 0
	}
	sep := string(filepath.Separator)
	parts := strings.Split(dir, sep)
	var window []string // 由内向外
	for i := len(parts) - 1; i >= 0 && len(window) < dirWindowDepth; i-- {
		comp := strings.TrimSpace(parts[i])
		if comp == "" || comp == "." {
			continue
		}
		window = append(window, parts[i])
	}

	changed := false
	seasonConflict := false
	kept := make([]string, 0, len(window))
	for _, raw := range window {
		norm := foldWidthAndT2S(strings.TrimSpace(raw))
		if n, ok := dirSeasonSegment(norm); ok {
			changed = true
			if n > 0 {
				if season == 0 {
					season = n
				} else if season != n {
					seasonConflict = true
				}
			}
			continue
		}
		if extraGenericDirNames[strings.ToLower(norm)] {
			changed = true
			continue
		}
		if c, ok := cleanBatchDirComponent(norm); ok {
			changed = true
			kept = append(kept, c)
			continue
		}
		kept = append(kept, raw)
	}
	if seasonConflict {
		season = 0
	}
	if !changed {
		return dir, 0
	}
	// 还原为由外向内的顺序
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return strings.Join(kept, sep), season
}

// dirSeasonSegment 判断整段是否为纯季目录；返回季号（Season 0 / S00 视为特典季，季号 0 但仍剔除）。
func dirSeasonSegment(comp string) (int, bool) {
	if m := reDirSeasonEn.FindStringSubmatch(comp); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	if m := reDirSeasonOrd.FindStringSubmatch(comp); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	if m := reDirSeasonCN.FindStringSubmatch(comp); m != nil {
		return parseChineseOrASCIINum(m[1]), true
	}
	return 0, false
}

// cleanBatchDirComponent 清洗批量目录名：复用 classifyBlocks 的括号分类，
// 去掉字幕组/规格/来源/语言/CRC/集数区间块，保留标题块与括号外正文。
// 年份块改写为 (YYYY)、媒体块原样保留，交给 parseDirectory 照旧提取并从标题剥离。
// 无括号块、未去掉任何噪声块或清洗后无标题时返回 ok=false（保持原段）。
func cleanBatchDirComponent(comp string) (string, bool) {
	blocks, rest := splitBracketBlocks(comp)
	if len(blocks) == 0 {
		return "", false
	}
	var pieces, tail []string
	hasTitle, dropped := false, false
	for _, b := range classifyBlocks(blocks, rest) {
		switch b.kind {
		case blockTitle:
			if reDirRangeBlock.MatchString(b.text) {
				dropped = true
				continue
			}
			pieces = append(pieces, b.text)
			hasTitle = true
		case blockYear:
			tail = append(tail, fmt.Sprintf("(%d)", b.year))
		case blockCategory:
			if mk, _ := stripMedia(b.text); mk != KindUnknown {
				tail = append(tail, b.text)
			} else {
				dropped = true
			}
		default:
			dropped = true
		}
	}
	if r := cleanTitleSpaces(rest); r != "" {
		pieces = append(pieces, r)
		hasTitle = true
	}
	pieces = append(pieces, tail...)
	// 只有确实去掉了噪声块才改写；纯标题括号目录（如 [K-ON!]）保持原样。
	if !hasTitle || !dropped {
		return "", false
	}
	out := cleanTitleSpaces(strings.Join(pieces, " "))
	if out == "" || out == comp {
		return "", false
	}
	return out, true
}

// applyDirSeasonFallback 标题取自目录、文件无季号且 dirSeason>0 时设置季号
// （修复纯数字 05.mkv 在 Season 2 / 第二季 目录下丢季号）。
// 文件已有不同季号时不覆盖，追加冲突走 Review（与文件标题分支同一文案）。
func applyDirSeasonFallback(ev *FileEvidence, dirSeason int) {
	if ev == nil || dirSeason <= 0 || len(ev.Titles) == 0 {
		return
	}
	for _, t := range ev.Titles {
		if t.Source != SourceDirectory {
			return
		}
	}
	if ev.Ordinal.Season == 0 {
		ev.Ordinal.Season = dirSeason
		return
	}
	if ev.Ordinal.Season != dirSeason {
		msg := fmt.Sprintf("目录季号%d与文件季号%d冲突", dirSeason, ev.Ordinal.Season)
		for _, c := range ev.Conflicts {
			if c == msg {
				return
			}
		}
		ev.Conflicts = append(ev.Conflicts, msg)
	}
}
