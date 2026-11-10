package matching

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// 预抽取：现有 classifyBlocks / parseRest 不认识、但语义明确的集号 / 季+集 / 总集数 token。
// 原则“明确才收”：纯数字括号（[86]/[12]/[2023]）、规格（[1080P]/[x264]）一律不动，
// 交由现有 isClearEpisodeBracket / isAmbiguousNumericBracket 规则处理。
var (
	// 括号内整块匹配（块内容已 TrimSpace）
	reTokSxxEyy      = regexp.MustCompile(`(?i)^S(\d{1,2})\s*E(\d{1,4})(?:v\d+)?$`)
	reTokSDashEp     = regexp.MustCompile(`(?i)^S(\d{1,2})\s*[-–—]\s*(\d{1,4})(?:v\d+)?$`)
	reTokOrdDashEp   = regexp.MustCompile(`(?i)^(\d{1,2})(?:st|nd|rd|th)(?:\s*season)?\s*[-–—]\s*(\d{1,4})(?:v\d+)?$`)
	reTokSeasonDash  = regexp.MustCompile(`(?i)^season\s*(\d{1,2})\s*[-–—]\s*(\d{1,4})(?:v\d+)?$`)
	reTokCNSeasonEp  = regexp.MustCompile(`^第\s*([0-9一二三四五六七八九十]+)\s*[季期]\s*[-–—]?\s*(?:第\s*)?(\d{1,4})\s*[话話集]?(?:v\d+)?$`)
	reTokCNEp        = regexp.MustCompile(`^第\s*(\d{1,4})\s*[话話集](?:v\d+)?$`)
	reTokEpPrefix    = regexp.MustCompile(`(?i)^(?:ep\.?\s*|episode\s*)(\d{1,4})(?:v\d+)?$`)
	reTokEpEnd       = regexp.MustCompile(`(?i)^(\d{1,4})(?:v\d+)?\s*(?:end|fin|完|完结|终)$`)
	reTokEpParenEnd  = regexp.MustCompile(`(?i)^(\d{1,4})(?:v\d+)?\s*\(\s*(?:end|fin|完|完结|终)\s*\)$`)
	reTokAbs         = regexp.MustCompile(`^总\s*第?\s*(\d{1,4})\s*[话話集]?$`)
	reBareEpToken    = regexp.MustCompile(`(?i)(?:^|[\s._])((?:[-–—]\s*)?(?:ep\.?\s*|episode\s*|e)(\d{1,4})(?:v\d+)?)(?:$|[\s._\[【(])`)
	reLeadingNumDash = regexp.MustCompile(`^\s*(\d{1,4})(?:v\d+)?\s*[-–—]\s+`)
	reAllDigits      = regexp.MustCompile(`^\d{1,4}$`)
)

// episodeHit 一次命中：[start,end) 为原串中要移除的区间。
type episodeHit struct {
	start, end int
	season     int
	episode    int
	abs        int
	note       string
}

// extractEpisodeTokens 预抽取括号集号、季+集组合、总集数与裸文件名集号；命中的 token 从返回串中移除。
// 若移除后现有解析器会从剩余文本读出另一个集号（如 [86][EP05] 中的 86、Mob Psycho 100 EP05 中的 100），
// 改为在原位写入规范明确集号块 [NN]，让现有 anyClearEp / keepTrailingTitleNumber 规则保护标题数字。
func extractEpisodeTokens(stem string, ex *fileExtras) string {
	if ex == nil || strings.TrimSpace(stem) == "" {
		return stem
	}
	hits := bracketEpisodeHits(stem)
	epHits := 0
	for _, h := range hits {
		if h.episode > 0 {
			epHits++
		}
	}
	if epHits == 0 {
		if h, ok := bareEpisodeHit(stem); ok {
			hits = append(hits, h)
			epHits = 1
		}
	}
	if len(hits) == 0 {
		return stem
	}
	// 多个不同集号互斥：保守起见不抽取任何集号（季号/总集数仍可用）。
	episode := 0
	for _, h := range hits {
		if h.episode == 0 {
			continue
		}
		if episode != 0 && episode != h.episode {
			ex.Notes = append(ex.Notes, fmt.Sprintf("预抽取集号互斥%d/%d，放弃", episode, h.episode))
			kept := hits[:0]
			for _, k := range hits {
				if k.episode == 0 {
					kept = append(kept, k)
				}
			}
			hits = kept
			episode = 0
			break
		}
		episode = h.episode
	}
	season := 0
	for _, h := range hits {
		if h.season > 0 {
			if season != 0 && season != h.season {
				// 互斥的显式季号是硬冲突：原始标记随后会被移除，必须进 Conflicts 走 Review，不能只写诊断 Notes。
				msg := fmt.Sprintf("预抽取季号互斥%d/%d", season, h.season)
				ex.Notes = append(ex.Notes, msg)
				ex.Conflicts = append(ex.Conflicts, msg)
				continue
			}
			season = h.season
		}
	}
	if len(hits) == 0 {
		return stem
	}

	out := removeHits(stem, hits, "")
	if episode > 0 {
		if sim := simulateEpisode(out); !isUnknownEp(sim) && !(sim.Kind == NumberSingle && sim.First == episode && sim.Last == episode) {
			out = removeHits(stem, hits, fmt.Sprintf("[%02d]", episode))
			ex.Notes = append(ex.Notes, fmt.Sprintf("剩余文本含集号%s，原位写入明确集号块", sim.Raw))
		}
		ex.Episode = episode
	}
	if season > 0 {
		ex.Season = season
	}
	for _, h := range hits {
		if h.abs > 0 {
			ex.Abs = h.abs
		}
		ex.Notes = append(ex.Notes, h.note)
	}
	return out
}

// bracketEpisodeHits 扫描 [] / 【】 块，整块匹配才收。
func bracketEpisodeHits(stem string) []episodeHit {
	var hits []episodeHit
	for _, loc := range reBracket.FindAllStringSubmatchIndex(stem, -1) {
		b := strings.TrimSpace(stem[loc[2]:loc[3]])
		if b == "" {
			continue
		}
		h, ok := classifyEpisodeBracket(b)
		if !ok {
			continue
		}
		h.start, h.end = loc[0], loc[1]
		h.note = "bracket:" + b
		hits = append(hits, h)
	}
	return hits
}

func classifyEpisodeBracket(b string) (episodeHit, bool) {
	var h episodeHit
	atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
	if m := reTokSxxEyy.FindStringSubmatch(b); m != nil {
		h.season, h.episode = atoi(m[1]), atoi(m[2])
	} else if m := reTokSDashEp.FindStringSubmatch(b); m != nil {
		h.season, h.episode = atoi(m[1]), atoi(m[2])
	} else if m := reTokOrdDashEp.FindStringSubmatch(b); m != nil {
		h.season, h.episode = atoi(m[1]), atoi(m[2])
	} else if m := reTokSeasonDash.FindStringSubmatch(b); m != nil {
		h.season, h.episode = atoi(m[1]), atoi(m[2])
	} else if m := reTokCNSeasonEp.FindStringSubmatch(b); m != nil {
		h.season, h.episode = parseChineseOrASCIINum(m[1]), atoi(m[2])
		if h.season == 0 {
			return h, false
		}
	} else if m := reTokCNEp.FindStringSubmatch(b); m != nil {
		h.episode = atoi(m[1])
	} else if m := reTokEpPrefix.FindStringSubmatch(b); m != nil {
		h.episode = atoi(m[1])
	} else if m := reTokEpEnd.FindStringSubmatch(b); m != nil {
		h.episode = atoi(m[1])
		if isYearAlone(h.episode) {
			return h, false
		}
	} else if m := reTokEpParenEnd.FindStringSubmatch(b); m != nil {
		h.episode = atoi(m[1])
		if isYearAlone(h.episode) {
			return h, false
		}
	} else if m := reTokAbs.FindStringSubmatch(b); m != nil {
		h.abs = atoi(m[1])
		return h, h.abs > 0
	} else {
		return h, false
	}
	if h.episode <= 0 {
		return h, false
	}
	return h, true
}

// bareEpisodeHit 括号外的裸集号：Title EP05 / Title - E05 / Episode 01 / EP01、
// 01 - 副标题、无空格 scene 名中“首个元数据 token 前紧邻的数字”（Title_Name_05_BD / Title.Name.05.1080p）。
func bareEpisodeHit(stem string) (episodeHit, bool) {
	spans := reBracket.FindAllStringIndex(stem, -1)
	inBracket := func(i int) bool {
		for _, s := range spans {
			if i >= s[0] && i < s[1] {
				return true
			}
		}
		return false
	}

	// ① EP / Episode / E 前缀；多处命中视为不明确
	var found []episodeHit
	for _, loc := range reBareEpToken.FindAllStringSubmatchIndex(stem, -1) {
		if inBracket(loc[2]) {
			continue
		}
		n, _ := strconv.Atoi(stem[loc[4]:loc[5]])
		if n <= 0 {
			continue
		}
		start, end := loc[2], loc[3]
		// 连同 token 前紧邻的“ - ”连接符一起移除
		for start > 0 && (stem[start-1] == ' ' || stem[start-1] == '-') {
			start--
		}
		found = append(found, episodeHit{start: start, end: end, episode: n, note: "bare:" + stem[loc[2]:loc[3]]})
	}
	if len(found) == 1 {
		return found[0], true
	}
	if len(found) > 1 {
		return episodeHit{}, false
	}
	if len(spans) > 0 {
		return episodeHit{}, false
	}

	// ② 01 - 副标题：开头数字 + 连接符，余下无数字
	if m := reLeadingNumDash.FindStringSubmatchIndex(stem); m != nil {
		n, _ := strconv.Atoi(stem[m[2]:m[3]])
		tail := strings.TrimSpace(stem[m[1]:])
		if n > 0 && !isYearAlone(n) && tail != "" && !strings.ContainsAny(tail, "0123456789") {
			return episodeHit{start: m[0], end: m[1], episode: n, note: "lead:" + strings.TrimSpace(stem[m[0]:m[1]])}, true
		}
	}

	// ③ 无空格 scene：以 _ 或 . 分隔
	if strings.ContainsAny(stem, " \t") {
		return episodeHit{}, false
	}
	for _, sep := range []string{"_", "."} {
		if h, ok := sceneSeparatedEpisode(stem, sep); ok {
			return h, true
		}
	}
	return episodeHit{}, false
}

func isTokenSep(c byte) bool {
	return c == ' ' || c == '.' || c == '_' || c == '\t'
}

// sceneSeparatedEpisode 在 sep 分隔的 token 中，找到首个元数据 token；其前一个 token 为纯数字、
// 再前一个 token 非数字时，取该数字为集号。年份、连续数字（12.5 / 2019.05）不收。
// 数字之后连续的纯元数据 token（_BD_1080p）一并移除，避免残留进标题。
func sceneSeparatedEpisode(stem, sep string) (episodeHit, bool) {
	parts := strings.Split(stem, sep)
	if len(parts) < 3 {
		return episodeHit{}, false
	}
	metaIdx := -1
	for i, p := range parts {
		if isSceneMetaToken(p) {
			metaIdx = i
			break
		}
	}
	if metaIdx < 2 {
		return episodeHit{}, false
	}
	num := parts[metaIdx-1]
	prev := parts[metaIdx-2]
	if !reAllDigits.MatchString(num) || prev == "" || isAllDigits(prev) {
		return episodeHit{}, false
	}
	n, _ := strconv.Atoi(num)
	if n <= 0 || isYearAlone(n) {
		return episodeHit{}, false
	}
	// 定位数字 token（含其前导分隔符），并连同其后连续的纯元数据 token 一起移除
	start := len(strings.Join(parts[:metaIdx-1], sep))
	end := start + len(sep) + len(num)
	for i := metaIdx; i < len(parts) && isSceneMetaToken(parts[i]); i++ {
		end += len(sep) + len(parts[i])
	}
	if strings.HasPrefix(stem[end:], sep) {
		end += len(sep)
	}
	return episodeHit{start: start, end: end, episode: n, note: "scene:" + num}, true
}

func isSceneMetaToken(p string) bool {
	low := strings.ToLower(strings.TrimSpace(p))
	if low == "" {
		return false
	}
	if reRes.MatchString(low) || reSceneResTok.MatchString(low) || codecTokens[low] || sourceTokens[low] || packagingTokens[low] || langTokens[low] {
		return true
	}
	return reCRC.MatchString(low) && !isAllDigits(low)
}

// removeHits 按区间移除命中 token；episode 命中处可写入 marker（空串表示直接移除）。
func removeHits(stem string, hits []episodeHit, marker string) string {
	var b strings.Builder
	last := 0
	ordered := append([]episodeHit(nil), hits...)
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && ordered[j].start < ordered[j-1].start; j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	for _, h := range ordered {
		if h.start < last {
			continue
		}
		b.WriteString(stem[last:h.start])
		if h.episode > 0 && marker != "" {
			b.WriteString(" " + marker + " ")
			marker = "" // 只写一次
		} else {
			b.WriteString(" ")
		}
		last = h.end
	}
	b.WriteString(stem[last:])
	return strings.Join(strings.Fields(b.String()), " ")
}

// simulateEpisode 复用现有 splitBracketBlocks / classifyBlocks / parseRest / pickEpisode，
// 预估清洗后串会被读出的集号，用于判断移除 token 是否会暴露标题数字。
func simulateEpisode(stem string) EpisodeToken {
	blocks, rest := splitBracketBlocks(stem)
	var cands []EpisodeToken
	for _, b := range classifyBlocks(blocks, rest) {
		if b.kind == blockEpisode {
			cands = append(cands, b.ep)
		}
	}
	rest = strings.TrimSpace(rest)
	if rest != "" {
		hasExplicitEp := false
		for _, e := range cands {
			if e.Explicit && !isUnknownEp(e) {
				hasExplicitEp = true
				break
			}
		}
		if rEp, _, _, _, _ := parseRest(rest, hasExplicitEp); !isUnknownEp(rEp) {
			cands = append(cands, rEp)
		}
	}
	return pickEpisode(cands, stem)
}
