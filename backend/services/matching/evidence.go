package matching

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// AnalyzeTitle 从标题抽取结构事实；文件与候选共用。保护标题数字。
func AnalyzeTitle(text string) TitleFacts {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return TitleFacts{}
	}
	work := foldWidthAndT2S(normLike(raw))

	var facts TitleFacts
	var conflicts []string

	// Media markers
	facts.Media, work = stripMedia(work)

	// Year in parentheses or standalone 1900-2099 when not episode context
	facts.Year, work = stripYearFromTitle(work)

	// Season / Part / Final / Roman / 期
	ord, work2, seasonConflicts := stripOrdinal(work)
	facts.Ordinal = ord
	work = work2
	conflicts = append(conflicts, seasonConflicts...)

	work = cleanTitleSpaces(work)
	facts.Base = work
	facts.Discriminators = collectDiscriminators(raw, work)
	facts.Conflicts = conflicts
	return facts
}

func normLike(s string) string {
	// lightweight: fullwidth digits already folded in foldWidthAndT2S callers;
	// keep original script; NFKC applied via Normalize elsewhere.
	return s
}

// ParseFile 解析并分别记录文件、目录证据；标题结构调用 AnalyzeTitle。
func ParseFile(input FileInput) FileEvidence {
	ev := FileEvidence{
		Name:  input.Name,
		Media: KindUnknown,
	}
	stem := strings.TrimSuffix(input.Name, filepath.Ext(input.Name))
	stem = foldWidthAndT2S(stem)

	blocks, rest := splitBracketBlocks(stem)
	classified := classifyBlocks(blocks)

	var titlePieces []string
	var seasons []int
	var years []int
	var epCandidates []EpisodeToken
	media := KindUnknown
	var conflicts []string

	for _, b := range classified {
		switch b.kind {
		case blockFansub, blockCodec, blockSource, blockLang, blockRes, blockCRC:
			// noise
		case blockCategory:
			if media == KindUnknown {
				if mk, rest := stripMedia(b.text); mk != KindUnknown && strings.TrimSpace(rest) == "" {
					media = mk
				} else if mediaBracketOnly(b.text) {
					switch strings.ToLower(strings.TrimSpace(b.text)) {
					case "tv":
						media = KindSeries
					case "ova", "oad":
						media = KindOVA
					case "movie", "film", "剧场版", "劇場版", "电影", "電影":
						media = KindMovie
					}
				}
			}
		case blockYear:
			years = append(years, b.year)
		case blockEpisode:
			epCandidates = append(epCandidates, b.ep)
		case blockTitle:
			titlePieces = append(titlePieces, b.text)
			// title may embed season/year
			tf := AnalyzeTitle(b.text)
			if tf.Ordinal.Season > 0 {
				seasons = append(seasons, tf.Ordinal.Season)
			}
			if tf.Year > 0 {
				years = append(years, tf.Year)
			}
			if tf.Media != KindUnknown && media == KindUnknown {
				media = tf.Media
			}
			conflicts = append(conflicts, tf.Conflicts...)
		}
	}

	// Rest outside brackets: scene / English titles / - ep
	rest = strings.TrimSpace(rest)
	if rest != "" {
		// 方括号等已给出明确集号时，不再把标题末尾续作数字当裸集号剥掉。
		hasExplicitEp := false
		for _, e := range epCandidates {
			if e.Explicit && !isUnknownEp(e) {
				hasExplicitEp = true
				break
			}
		}
		rEp, rRest, rYear, rSeason, rMedia := parseRest(rest, hasExplicitEp)
		if !isUnknownEp(rEp) {
			epCandidates = append(epCandidates, rEp)
		}
		if rYear > 0 {
			years = append(years, rYear)
		}
		if rSeason > 0 {
			seasons = append(seasons, rSeason)
		}
		if rMedia != KindUnknown && media == KindUnknown {
			media = rMedia
		}
		rRest = cleanTitleSpaces(rRest)
		if rRest != "" {
			titlePieces = append(titlePieces, rRest)
			tf := AnalyzeTitle(rRest)
			if tf.Ordinal.Season > 0 {
				seasons = append(seasons, tf.Ordinal.Season)
			}
			if tf.Media != KindUnknown && media == KindUnknown {
				media = tf.Media
			}
			conflicts = append(conflicts, tf.Conflicts...)
		}
	}

	// Titles: split language families; keep original language text (pre-t2s for display? keep as parsed)
	ev.Titles = buildTitles(titlePieces, SourceFile)

	// Merge seasons: Chinese 第N季 vs Roman must agree
	season, seasonConflict := mergeSeasons(seasons, titlePieces)
	if seasonConflict != "" {
		conflicts = append(conflicts, seasonConflict)
	}
	ev.Ordinal.Season = season

	year, yearConflict := mergeYears(years)
	if yearConflict != "" {
		conflicts = append(conflicts, yearConflict)
	}
	ev.Year = year
	ev.Media = media
	ev.Episode = pickEpisode(epCandidates, stem)
	ev.Conflicts = uniqueStrings(conflicts)

	// Pure numbering filename (05.mkv): title equals episode → clear for directory fallback.
	if len(ev.Titles) == 1 {
		if n, err := strconv.Atoi(strings.TrimSpace(ev.Titles[0].Text)); err == nil && n > 0 {
			if isUnknownEp(ev.Episode) {
				ev.Episode = EpisodeToken{Kind: NumberSingle, First: n, Last: n, Raw: ev.Titles[0].Text, Explicit: false}
				ev.Titles = nil
			} else if ev.Episode.Kind == NumberSingle && ev.Episode.First == n {
				ev.Titles = nil
			}
		}
	}

	// Directory evidence
	dirTitles, dirSeason, dirYear, dirMedia := parseDirectory(input.Dir)
	if len(ev.Titles) == 0 {
		for _, t := range dirTitles {
			ev.Titles = append(ev.Titles, Title{Text: t, Source: SourceDirectory})
		}
	} else {
		// Directory may supplement season/year when file lacks them; conflicts Review
		if dirSeason > 0 {
			if ev.Ordinal.Season == 0 {
				ev.Ordinal.Season = dirSeason
			} else if ev.Ordinal.Season != dirSeason {
				ev.Conflicts = append(ev.Conflicts, fmt.Sprintf("目录季号%d与文件季号%d冲突", dirSeason, ev.Ordinal.Season))
			}
		}
		if dirYear > 0 {
			if ev.Year == 0 {
				ev.Year = dirYear
			} else if ev.Year != dirYear {
				ev.Conflicts = append(ev.Conflicts, fmt.Sprintf("目录年份%d与文件年份%d冲突", dirYear, ev.Year))
			}
		}
		if ev.Media == KindUnknown && dirMedia != KindUnknown {
			ev.Media = dirMedia
		}
	}
	// Part/Final from any title analyze
	for _, t := range ev.Titles {
		tf := AnalyzeTitle(t.Text)
		if tf.Ordinal.Part > 0 && ev.Ordinal.Part == 0 {
			ev.Ordinal.Part = tf.Ordinal.Part
		}
		if tf.Ordinal.Final {
			ev.Ordinal.Final = true
		}
	}

	// Strip season/ep from title bases for cleaner Titles? Keep original title text
	// but also store analyzed bases by rewriting Titles to base form for grouping.
	// Contract: "Season/Part/Final/Media剥离后仍保留原始Title用于精确区分"
	// So Titles keep original; grouping uses AnalyzeTitle(base).

	return ev
}

// --- block classification ---

type blockKind int

const (
	blockUnknown blockKind = iota
	blockFansub
	blockCategory
	blockCodec
	blockSource
	blockLang
	blockRes
	blockCRC
	blockYear
	blockEpisode
	blockTitle
)

type classifiedBlock struct {
	kind blockKind
	text string
	year int
	ep   EpisodeToken
}

var (
	reBracket   = regexp.MustCompile(`[\[【]([^\]】]*)[\]】]`)
	reSxxEyy    = regexp.MustCompile(`(?i)\bS(\d{1,2})E(\d{1,4})\b`)
	reEpCN      = regexp.MustCompile(`第\s*(\d{1,4})\s*[集話话]`)
	reEpZero    = regexp.MustCompile(`第\s*0+\s*[集話话]`)
	reSeasonCN  = regexp.MustCompile(`第\s*([0-9一二三四五六七八九十百]+)\s*[季期]`)
	reSeasonEn  = regexp.MustCompile(`(?i)\b(?:season\s*(\d{1,2})|(\d{1,2})(?:st|nd|rd|th)\s*season|s(\d{1,2}))\b`)
	rePart      = regexp.MustCompile(`(?i)\bpart\s*(\d{1,2})\b|第\s*([0-9一二三四五六七八九十]+)\s*部`)
	reFinal     = regexp.MustCompile(`(?i)\bfinal\b|完结篇|最終回|最终回`)
	reYearPar   = regexp.MustCompile(`\((19|20)\d{2}\)`)
	reYearTok   = regexp.MustCompile(`\b((?:19|20)\d{2})\b`)
	reRange     = regexp.MustCompile(`(?i)(?:^|[\s\-_])(\d{1,4})\s*[-~～]\s*(\d{1,4})(?:\b|$)`)
	reFraction  = regexp.MustCompile(`(?i)(?:^|[\s\-_])(\d{1,4}\.\d+)(?:\b|$)`)
	reDashEp    = regexp.MustCompile(`(?i)(?:^|[\s])[-–—]\s*(\d{1,4})(?:v\d+)?(?:\b|$)`)
	reBareEp    = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])(\d{1,4})(?:v\d+)?$`)
	reCRC       = regexp.MustCompile(`(?i)^[0-9A-F]{8}$`)
	reRes       = regexp.MustCompile(`(?i)^(?:\d{3,4}\s*[xX×]\s*\d{3,4}|\d{3,4}p|4k|2160p|1080p|720p|480p)$`)
	reEpBracket = regexp.MustCompile(`(?i)^(?:e?(\d{1,4})(?:v\d+)?)$`)
)

var categoryExact = map[string]bool{
	"国漫": true, "日漫": true, "动漫": true, "动画": true, "番剧": true,
	"anime": true, "donghua": true,
}

var codecTokens = map[string]bool{
	"avc": true, "hevc": true, "h264": true, "h265": true, "h.264": true, "h.265": true,
	"x264": true, "x265": true, "av1": true, "vp9": true, "10bit": true, "8bit": true,
	"10-bit": true, "8-bit": true, "aac": true, "flac": true, "mp3": true, "opus": true,
	"ac3": true, "mkv": true, "mp4": true, "webm": true,
}

var sourceTokens = map[string]bool{
	"webrip": true, "webdl": true, "web-dl": true, "bdrip": true, "bluray": true,
	"blu-ray": true, "remux": true, "hdtv": true, "cr": true, "bilibili": true,
	"crunchyroll": true, "netflix": true, "nf": true, "amzn": true, "dsnp": true,
}

var langTokens = map[string]bool{
	"gb": true, "big5": true, "chs": true, "cht": true, "jpsc": true, "gbsc": true,
	"简体": true, "繁体": true, "简繁": true, "中字": true, "内封": true, "内嵌": true, "外挂": true,
	"sc": true, "tc": true, "jp": true, "en": true,
}

var fansubHints = []string{"team", "sub", "字幕", "字幕组", "encodes", "encode"}

func splitBracketBlocks(stem string) (blocks []string, rest string) {
	rest = stem
	for {
		loc := reBracket.FindStringSubmatchIndex(rest)
		if loc == nil {
			break
		}
		content := rest[loc[2]:loc[3]]
		blocks = append(blocks, content)
		rest = rest[:loc[0]] + " " + rest[loc[1]:]
	}
	return blocks, rest
}

func classifyBlocks(blocks []string) []classifiedBlock {
	out := make([]classifiedBlock, 0, len(blocks))
	for i, raw := range blocks {
		b := strings.TrimSpace(raw)
		if b == "" {
			continue
		}
		lower := strings.ToLower(b)
		cb := classifiedBlock{text: b}

		if categoryExact[b] || categoryExact[lower] {
			cb.kind = blockCategory
			out = append(out, cb)
			continue
		}
		if reCRC.MatchString(b) {
			cb.kind = blockCRC
			out = append(out, cb)
			continue
		}
		if reRes.MatchString(strings.ReplaceAll(b, " ", "")) {
			cb.kind = blockRes
			out = append(out, cb)
			continue
		}
		if yearBracketContent(b) {
			cb.kind = blockYear
			cb.year, _ = strconv.Atoi(b)
			out = append(out, cb)
			continue
		}
		if m := reEpBracket.FindStringSubmatch(b); m != nil {
			n, _ := strconv.Atoi(m[1])
			if n > 0 {
				cb.kind = blockEpisode
				cb.ep = EpisodeToken{Kind: NumberSingle, First: n, Last: n, Raw: b, Explicit: true}
				out = append(out, cb)
				continue
			}
		}
		if codecTokens[lower] || hasAnyToken(lower, codecTokens) {
			cb.kind = blockCodec
			out = append(out, cb)
			continue
		}
		if sourceTokens[lower] || hasAnyToken(lower, sourceTokens) {
			cb.kind = blockSource
			out = append(out, cb)
			continue
		}
		if langTokens[lower] || langTokens[b] {
			cb.kind = blockLang
			out = append(out, cb)
			continue
		}
		// 纯媒体标记优先于字幕组启发式（避免 [TV] 被当成 fansub）
		if mk, rest := stripMedia(b); mk != KindUnknown && strings.TrimSpace(rest) == "" {
			cb.kind = blockCategory
			out = append(out, cb)
			continue
		}
		if mediaBracketOnly(b) {
			cb.kind = blockCategory
			out = append(out, cb)
			continue
		}
		// First bracket：允许短拉丁组名（含 Dynamis One）
		if i == 0 && looksLikeLeadingFansub(b) {
			cb.kind = blockFansub
			out = append(out, cb)
			continue
		}
		// Non-first short team tokens
		if looksLikeFansub(b) && !hasCJK(b) && !hasTitleDigitIdentity(b) {
			cb.kind = blockFansub
			out = append(out, cb)
			continue
		}
		cb.kind = blockTitle
		out = append(out, cb)
	}
	return out
}

func mediaBracketOnly(b string) bool {
	switch strings.ToLower(strings.TrimSpace(b)) {
	case "tv", "ova", "oad", "movie", "film", "剧场版", "劇場版", "电影", "電影":
		return true
	}
	return false
}

func yearBracketContent(b string) bool {
	if len(b) != 4 {
		return false
	}
	n, err := strconv.Atoi(b)
	return err == nil && n >= 1900 && n <= 2099
}

func looksLikeFansub(b string) bool {
	lower := strings.ToLower(b)
	for _, h := range fansubHints {
		if strings.Contains(lower, h) {
			return true
		}
	}
	if hasCJK(b) {
		return false
	}
	// 无提示时，带空格的多词视为标题（Star River III），不作字幕组
	if strings.Contains(b, " ") {
		return false
	}
	if len([]rune(b)) <= 24 && regexp.MustCompile(`(?i)^[A-Za-z0-9][A-Za-z0-9._\-&+]{0,22}$`).MatchString(b) {
		return true
	}
	return false
}

// looksLikeLeadingFansub 仅用于文件名首个括号：字幕组常为 1～3 个拉丁词（Dynamis One）。
// 含罗马季号或中文季号的不视为组名。
func looksLikeLeadingFansub(b string) bool {
	if looksLikeFansub(b) {
		return true
	}
	if hasCJK(b) || mediaBracketOnly(b) {
		return false
	}
	if _, ok := findRomanSeason(b); ok {
		return false
	}
	if reSeasonCN.MatchString(b) || reSeasonEn.MatchString(b) {
		return false
	}
	fields := strings.Fields(b)
	if len(fields) == 0 || len(fields) > 3 {
		return false
	}
	if len([]rune(b)) > 28 {
		return false
	}
	for _, f := range fields {
		for _, r := range f {
			if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == '&' || r == '+') {
				return false
			}
		}
	}
	return true
}

func hasAnyToken(lower string, set map[string]bool) bool {
	for _, f := range strings.FieldsFunc(lower, func(r rune) bool {
		return r == ' ' || r == '/' || r == ',' || r == '+' || r == '&'
	}) {
		if set[f] {
			return true
		}
	}
	return false
}

func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.In(r, unicode.Hiragana, unicode.Katakana) {
			return true
		}
	}
	return false
}

func hasTitleDigitIdentity(s string) bool {
	// 86, Ranma1-2, U-17, No.8 — treat as title not fansub
	if regexp.MustCompile(`(?i)\b(?:no\.?\s*\d+|u-\d+|\d{2,})\b`).MatchString(s) {
		return true
	}
	return false
}

func parseRest(rest string, keepTrailingTitleNumber bool) (ep EpisodeToken, title string, year, season int, media MediaKind) {
	ep = EpisodeToken{Kind: NumberUnknown}
	work := rest

	// Parenthetical metadata dump: (CR 1920x1080 AVC AAC MKV)
	work = stripParenMeta(work)

	// SxxEyy
	if m := reSxxEyy.FindStringSubmatch(work); m != nil {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		season = s
		ep = EpisodeToken{Kind: NumberSingle, First: e, Last: e, Raw: m[0], Explicit: true}
		work = reSxxEyy.ReplaceAllString(work, " ")
	}

	// 第0话 special before 第N话
	if reEpZero.MatchString(work) {
		ep = EpisodeToken{Kind: NumberSpecial, Raw: reEpZero.FindString(work), Explicit: true}
		work = reEpZero.ReplaceAllString(work, " ")
	} else if m := reEpCN.FindStringSubmatch(work); m != nil {
		n, _ := strconv.Atoi(m[1])
		ep = EpisodeToken{Kind: NumberSingle, First: n, Last: n, Raw: m[0], Explicit: true}
		work = reEpCN.ReplaceAllString(work, " ")
	}

	// Season CN / EN / Roman in rest
	if season == 0 {
		if m := reSeasonCN.FindStringSubmatch(work); m != nil {
			season = parseChineseOrASCIINum(m[1])
			work = reSeasonCN.ReplaceAllString(work, " ")
		}
	}
	if season == 0 {
		if m := reSeasonEn.FindStringSubmatch(work); m != nil {
			for i := 1; i < len(m); i++ {
				if m[i] != "" {
					season, _ = strconv.Atoi(m[i])
					break
				}
			}
			work = reSeasonEn.ReplaceAllString(work, " ")
		}
	}
	if season == 0 {
		if n, ok := romanSeasonAtEnd(work); ok {
			season = n
			work = stripTrailingRoman(work)
		}
	}

	// Year
	if m := reYearPar.FindStringSubmatch(work); m != nil {
		year, _ = strconv.Atoi(m[1] + m[0][3:5]) // careful
		// better:
		year, _ = strconv.Atoi(strings.Trim(m[0], "()"))
		work = reYearPar.ReplaceAllString(work, " ")
	}

	media, work = stripMedia(work)

	// Fraction then compact Range; spaced "Title 5 - 03" is dash-ep, not range.
	if isUnknownEp(ep) {
		if m := reFraction.FindStringSubmatch(work); m != nil {
			ep = EpisodeToken{Kind: NumberFraction, Raw: m[1], Explicit: true}
			work = strings.Replace(work, m[0], " ", 1)
		}
	}
	if isUnknownEp(ep) {
		if m := reRange.FindStringSubmatch(work); m != nil {
			a, _ := strconv.Atoi(m[1])
			b, _ := strconv.Atoi(m[2])
			inner := strings.TrimSpace(m[0])
			for len(inner) > 0 && (inner[0] < '0' || inner[0] > '9') {
				inner = inner[1:]
			}
			spaced := strings.Contains(inner, " ")
			if a > 0 && b > a && !spaced {
				ep = EpisodeToken{Kind: NumberRange, First: a, Last: b, Raw: m[1] + "-" + m[2], Explicit: true}
				work = strings.Replace(work, m[0], " ", 1)
			}
		}
	}

	if isUnknownEp(ep) {
		if m := reDashEp.FindStringSubmatch(work); m != nil {
			n, _ := strconv.Atoi(m[1])
			if n > 0 && !isTitleProtectedNumber(work, n) {
				ep = EpisodeToken{Kind: NumberSingle, First: n, Last: n, Raw: m[1], Explicit: true}
				work = reDashEp.ReplaceAllString(work, " ")
				// "Title 5 - 03" → season 5 when trailing small int remains
				if season == 0 {
					if sm := regexp.MustCompile(`(?i)(?:^|[\s._])([1-9]|1[0-9]|2[0-4])\s*$`).FindStringSubmatch(cleanTitleSpaces(work)); sm != nil {
						sn, _ := strconv.Atoi(sm[1])
						if sn > 0 && !isTitleProtectedNumber(work, sn) {
							season = sn
							work = regexp.MustCompile(`(?i)(?:^|[\s._])(?:`+sm[1]+`)\s*$`).ReplaceAllString(work, " ")
						}
					}
				}
			}
		}
	}

	// Trailing bare number last。已有明确集号时保留标题续作数字（如「悠久之翼2」「爆漫王。3」）。
	if isUnknownEp(ep) && !keepTrailingTitleNumber {
		workTrim := cleanTitleSpaces(work)
		if m := reBareEp.FindStringSubmatch(workTrim); m != nil {
			n, _ := strconv.Atoi(m[1])
			if n > 0 && !isYearAlone(n) && !isTitleProtectedNumber(workTrim, n) {
				ep = EpisodeToken{Kind: NumberSingle, First: n, Last: n, Raw: m[1], Explicit: false}
				work = strings.TrimSuffix(workTrim, m[1])
			}
		}
	}

	// Scene dots → spaces for latin titles
	work = strings.ReplaceAll(work, ".", " ")
	work = stripSceneNoiseTokens(work)
	title = cleanTitleSpaces(work)
	return ep, title, year, season, media
}

// stripSceneNoiseTokens 去掉 rest 中残留的分辨率/来源/编码噪声。
func stripSceneNoiseTokens(work string) string {
	fields := strings.Fields(work)
	if len(fields) == 0 {
		return work
	}
	out := fields[:0]
	for _, f := range fields {
		low := strings.ToLower(strings.Trim(f, "-_"))
		if reRes.MatchString(low) || sourceTokens[low] || codecTokens[low] {
			continue
		}
		if low == "web" || low == "dl" {
			continue
		}
		out = append(out, f)
	}
	return strings.Join(out, " ")
}

func stripParenMeta(s string) string {
	return regexp.MustCompile(`\(([^)]*)\)`).ReplaceAllStringFunc(s, func(m string) string {
		inner := strings.Trim(m, "()")
		lower := strings.ToLower(inner)
		// Keep (1999)/(2011) years
		if yearBracketContent(strings.TrimSpace(inner)) {
			return m
		}
		// Metadata dump if mostly codec/res/source tokens
		fields := strings.Fields(lower)
		meta := 0
		for _, f := range fields {
			f = strings.Trim(f, ",;")
			if codecTokens[f] || sourceTokens[f] || reRes.MatchString(f) || langTokens[f] {
				meta++
			}
		}
		if len(fields) > 0 && meta*2 >= len(fields) {
			return " "
		}
		return m
	})
}

func isYearAlone(n int) bool {
	return n >= 1900 && n <= 2099
}

// isTitleProtectedNumber: digits that are part of title identity (86, 1-2, U-17, No.8)。
// 不把 "Title - 1100" 这种空格分隔的 dash-ep 当成标题分数。
func isTitleProtectedNumber(title string, n int) bool {
	t := strings.ToLower(title)
	ns := strconv.Itoa(n)
	// 紧凑分数 Ranma1-2 / 1-2 / 1/2：连字符两侧无空格
	compactLeft := regexp.MustCompile(`(?i)(?:[a-z]+)?` + regexp.QuoteMeta(ns) + `[-/]\d+\b`)
	if compactLeft.MatchString(t) {
		return true
	}
	compactRight := regexp.MustCompile(`(?i)\d+[-/]` + regexp.QuoteMeta(ns) + `\b`)
	if compactRight.MatchString(t) {
		// 排除 "12 - 03" / "Tale - 1100" 这种两侧有空格的集号写法
		spaced := regexp.MustCompile(`\d+\s+[-–—]\s*` + regexp.QuoteMeta(ns) + `|[-–—]\s+` + regexp.QuoteMeta(ns))
		if !spaced.MatchString(t) {
			return true
		}
	}
	if strings.Contains(t, "u-"+ns) || strings.Contains(t, "no."+ns) || strings.Contains(t, "no "+ns) {
		return true
	}
	base := cleanTitleSpaces(reDashEp.ReplaceAllString(title, " "))
	base = cleanTitleSpaces(base)
	if base == ns {
		return true
	}
	return false
}

func isUnknownEp(ep EpisodeToken) bool {
	return ep.Kind == "" || ep.Kind == NumberUnknown
}

func pickEpisode(cands []EpisodeToken, stem string) EpisodeToken {
	if len(cands) == 0 {
		return EpisodeToken{Kind: NumberUnknown}
	}
	// Prefer Explicit Single, then any Explicit, then first
	var best EpisodeToken
	rank := func(e EpisodeToken) int {
		r := 0
		if e.Explicit {
			r += 10
		}
		switch e.Kind {
		case NumberSingle:
			r += 5
		case NumberRange, NumberFraction, NumberSpecial:
			r += 3
		}
		return r
	}
	bestRank := -1
	for _, e := range cands {
		if isUnknownEp(e) {
			continue
		}
		r := rank(e)
		if r > bestRank {
			bestRank = r
			best = e
		}
	}
	if bestRank < 0 {
		return EpisodeToken{Kind: NumberUnknown}
	}
	return best
}

func buildTitles(pieces []string, src Source) []Title {
	var out []Title
	seen := map[string]bool{}
	for _, p := range pieces {
		p = cleanTitleSpaces(p)
		if p == "" {
			continue
		}
		// Split parallel language chunks by ][ already done; also split " / " when script switch
		for _, part := range splitLanguageParts(p) {
			part = cleanTitleSpaces(part)
			part = normalizeTitleFractionHyphen(part)
			if part == "" {
				continue
			}
			// Remove trailing season markers from display title? Keep original for discriminators.
			key := TitleKey(part)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Title{Text: part, Source: src})
		}
	}
	return out
}

// normalizeTitleFractionHyphen 将标题内数字分数连字符写成斜杠（Ranma1-2 → Ranma1/2），
// 与 4.5 / 检索夹具 Ranma1/2 对齐；不把集号范围 01-02 改成斜杠。
func normalizeTitleFractionHyphen(s string) string {
	if s == "" || !strings.Contains(s, "-") {
		return s
	}
	// letter/digit glued fraction: Ranma1-2, 1-2 at word boundary after letters
	re := regexp.MustCompile(`(?i)([A-Za-z]+\d+)-(\d+)\b`)
	s = re.ReplaceAllString(s, `$1/$2`)
	re2 := regexp.MustCompile(`(?i)\b(\d+)-(\d+)\b`)
	// only bare N-M when both sides are single digit (title 1/2), not episode ranges like 01-02 / 12-13
	s = re2.ReplaceAllStringFunc(s, func(m string) string {
		parts := strings.Split(m, "-")
		if len(parts) != 2 {
			return m
		}
		if len(parts[0]) == 1 && len(parts[1]) == 1 {
			return parts[0] + "/" + parts[1]
		}
		return m
	})
	return s
}

func splitLanguageParts(s string) []string {
	// Do not split Fate/Zero or 1/2
	if !strings.Contains(s, "/") {
		return []string{s}
	}
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		// carefully: only split when clear script switch
		return maybeSplitScript(s)
	}
	a, b := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if a == "" || b == "" {
		return []string{s}
	}
	// 1/2 numeric fraction in title
	if isAllDigits(a) && isAllDigits(b) {
		return []string{s}
	}
	// Fate/Zero style latin/latin single tokens — keep
	if isLatinWord(a) && isLatinWord(b) && !hasCJK(a) && !hasCJK(b) {
		return []string{s}
	}
	return []string{s}
}

func maybeSplitScript(s string) []string {
	return []string{s}
}

func isLatinWord(s string) bool {
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '\'' || r == '.') {
			return false
		}
	}
	return s != ""
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func mergeSeasons(seasons []int, titles []string) (int, string) {
	// Also extract roman from titles for corroboration
	var romans []int
	for _, t := range titles {
		if n, ok := findRomanSeason(t); ok {
			romans = append(romans, n)
		}
		if m := reSeasonCN.FindStringSubmatch(t); m != nil {
			seasons = append(seasons, parseChineseOrASCIINum(m[1]))
		}
	}
	seasons = append(seasons, romans...)
	uniq := uniquePositive(seasons)
	if len(uniq) == 0 {
		return 0, ""
	}
	if len(uniq) > 1 {
		return 0, fmt.Sprintf("季号冲突：%v", uniq)
	}
	return uniq[0], ""
}

func mergeYears(years []int) (int, string) {
	uniq := uniquePositive(years)
	if len(uniq) == 0 {
		return 0, ""
	}
	if len(uniq) > 1 {
		return 0, fmt.Sprintf("年份冲突：%v", uniq)
	}
	return uniq[0], ""
}

func uniquePositive(xs []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, x := range xs {
		if x <= 0 || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}

func uniqueStrings(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		x = strings.TrimSpace(x)
		if x == "" || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}

// --- ordinal / media / year helpers for AnalyzeTitle ---

var romanMap = map[rune]int{
	'Ⅰ': 1, 'Ⅱ': 2, 'Ⅲ': 3, 'Ⅳ': 4, 'Ⅴ': 5,
	'Ⅵ': 6, 'Ⅶ': 7, 'Ⅷ': 8, 'Ⅸ': 9, 'Ⅹ': 10,
	'ⅰ': 1, 'ⅱ': 2, 'ⅲ': 3, 'ⅳ': 4, 'ⅴ': 5,
	'ⅵ': 6, 'ⅶ': 7, 'ⅷ': 8, 'ⅸ': 9, 'ⅹ': 10,
}

var romanASCII = []struct {
	tok string
	n   int
}{
	{"viii", 8}, {"vii", 7}, {"xii", 12}, {"xi", 11}, {"iii", 3}, {"ii", 2},
	{"iv", 4}, {"ix", 9}, {"vi", 6}, {"x", 10}, {"v", 5}, {"i", 1},
}

func stripOrdinal(work string) (Ordinal, string, []string) {
	var ord Ordinal
	var conflicts []string

	if reFinal.MatchString(work) {
		ord.Final = true
		work = reFinal.ReplaceAllString(work, " ")
	}
	if m := rePart.FindStringSubmatch(work); m != nil {
		if m[1] != "" {
			ord.Part, _ = strconv.Atoi(m[1])
		} else {
			ord.Part = parseChineseOrASCIINum(m[2])
		}
		work = rePart.ReplaceAllString(work, " ")
	}

	cnSeasons := []int{}
	for _, m := range reSeasonCN.FindAllStringSubmatch(work, -1) {
		cnSeasons = append(cnSeasons, parseChineseOrASCIINum(m[1]))
	}
	work = reSeasonCN.ReplaceAllString(work, " ")

	enSeasons := []int{}
	for _, m := range reSeasonEn.FindAllStringSubmatch(work, -1) {
		for i := 1; i < len(m); i++ {
			if m[i] != "" {
				n, _ := strconv.Atoi(m[i])
				if n > 0 {
					enSeasons = append(enSeasons, n)
				}
				break
			}
		}
	}
	work = reSeasonEn.ReplaceAllString(work, " ")

	romans := []int{}
	if n, ok := findRomanSeason(work); ok {
		romans = append(romans, n)
		work = stripAllRomans(work)
	}

	all := append(append(cnSeasons, enSeasons...), romans...)
	uniq := uniquePositive(all)
	if len(uniq) == 1 {
		ord.Season = uniq[0]
	} else if len(uniq) > 1 {
		conflicts = append(conflicts, fmt.Sprintf("季号冲突：%v", uniq))
	}
	return ord, cleanTitleSpaces(work), conflicts
}

func findRomanSeason(s string) (int, bool) {
	// Unicode roman chars
	for _, r := range s {
		if n, ok := romanMap[r]; ok {
			return n, true
		}
	}
	// ASCII roman as standalone token at end or whole
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0, false
	}
	last := strings.ToLower(fields[len(fields)-1])
	for _, cand := range romanASCII {
		if last == cand.tok {
			return cand.n, true
		}
	}
	return 0, false
}

func stripAllRomans(s string) string {
	var b strings.Builder
	for _, r := range s {
		if _, ok := romanMap[r]; ok {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	s = b.String()
	fields := strings.Fields(s)
	if len(fields) > 0 {
		last := strings.ToLower(fields[len(fields)-1])
		for _, cand := range romanASCII {
			if last == cand.tok {
				fields = fields[:len(fields)-1]
				break
			}
		}
	}
	return strings.Join(fields, " ")
}

func romanSeasonAtEnd(s string) (int, bool) {
	return findRomanSeason(s)
}

func stripTrailingRoman(s string) string {
	return stripAllRomans(s)
}

func stripMedia(work string) (MediaKind, string) {
	lower := strings.ToLower(work)
	patterns := []struct {
		re   *regexp.Regexp
		kind MediaKind
	}{
		{regexp.MustCompile(`(?i)剧场版|劇場版|\bmovie\b|\bfilm\b`), KindMovie},
		{regexp.MustCompile(`(?i)\bova\b|\boad\b`), KindOVA},
		{regexp.MustCompile(`(?i)总集篇|總集篇|\brecap\b`), KindRecap},
		{regexp.MustCompile(`(?i)\bpv\b|\bcm\b|预告|預告|\btrailer\b|\bpromo\b`), KindPromo},
	}
	kind := KindUnknown
	for _, p := range patterns {
		if p.re.MatchString(lower) {
			kind = p.kind
			work = p.re.ReplaceAllString(work, " ")
			break
		}
	}
	return kind, work
}

func stripYearFromTitle(work string) (int, string) {
	if m := reYearPar.FindStringSubmatch(work); m != nil {
		y, _ := strconv.Atoi(strings.Trim(m[0], "()"))
		work = reYearPar.ReplaceAllString(work, " ")
		return y, work
	}
	return 0, work
}

func collectDiscriminators(raw, base string) []string {
	var out []string
	// bang / double bang
	if strings.Contains(raw, "!!") || strings.Contains(base, "!!") {
		out = append(out, "!!")
	} else if strings.Contains(raw, "!") || strings.Contains(base, "!") {
		out = append(out, "!")
	}
	if strings.Contains(raw, ";") || strings.Contains(base, ";") {
		out = append(out, ";")
	}
	// trailing single letter season marks Railgun S/T
	fields := strings.Fields(base)
	if len(fields) >= 2 {
		last := fields[len(fields)-1]
		if len(last) == 1 && ((last[0] >= 'A' && last[0] <= 'Z') || (last[0] >= 'a' && last[0] <= 'z')) {
			out = append(out, last)
		}
	}
	// numeric identity tokens
	for _, m := range regexp.MustCompile(`(?i)(?:\b(?:no\.?\s*\d+|u-\d+)\b|\b\d+\b)`).FindAllString(base, -1) {
		out = append(out, m)
	}
	if strings.Contains(base, "1-2") || strings.Contains(base, "1/2") {
		out = append(out, "1-2")
	}
	return uniqueStrings(out)
}

func parseChineseOrASCIINum(s string) int {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return parseChineseNum(s)
}

func parseChineseNum(s string) int {
	digits := map[rune]int{
		'一': 1, '二': 2, '三': 3, '四': 4, '五': 5,
		'六': 6, '七': 7, '八': 8, '九': 9,
	}
	runes := []rune(s)
	switch len(runes) {
	case 0:
		return 0
	case 1:
		if runes[0] == '十' {
			return 10
		}
		return digits[runes[0]]
	case 2:
		if runes[0] == '十' {
			if v, ok := digits[runes[1]]; ok {
				return 10 + v
			}
			return 0
		}
		if runes[1] == '十' {
			if v, ok := digits[runes[0]]; ok {
				return v * 10
			}
			return 0
		}
		return 0
	case 3:
		if runes[1] == '十' {
			a, ok1 := digits[runes[0]]
			b, ok2 := digits[runes[2]]
			if ok1 && ok2 {
				return a*10 + b
			}
		}
		return 0
	default:
		return 0
	}
}

func cleanTitleSpaces(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.Trim(s, " -_")
}

// --- directory ---

var genericDirNames = map[string]bool{
	"anime": true, "animes": true, "animation": true,
	"新番": true, "完结": true, "完结番": true, "连载": true, "追番": true,
	"番剧": true, "动漫": true, "动画": true,
	"download": true, "downloads": true, "下载": true,
	"video": true, "videos": true, "视频": true, "media": true, "媒体": true,
	"tv": true, "movie": true, "movies": true, "电影": true, "剧场版": true,
	"bd": true, "bdrip": true, "web": true, "webrip": true, "webdl": true,
	"web-dl": true, "remux": true, "bluray": true, "blu-ray": true,
	"encode": true, "encodes": true, "高清": true, "超清": true, "蓝光": true,
	"sp": true, "special": true, "specials": true, "特典": true,
	"待整理": true, "未完成": true, "国漫": true, "日漫": true,
}

func parseDirectory(relDir string) (titles []string, season, year int, media MediaKind) {
	if relDir == "" {
		return nil, 0, 0, KindUnknown
	}
	parts := strings.Split(relDir, string(filepath.Separator))
	depth := 0
	for i := len(parts) - 1; i >= 0 && depth < 3; i-- {
		comp := strings.TrimSpace(parts[i])
		if comp == "" || comp == "." {
			continue
		}
		depth++
		norm := foldWidthAndT2S(comp)
		if season == 0 {
			if m := reSeasonCN.FindStringSubmatch(norm); m != nil {
				season = parseChineseOrASCIINum(m[1])
			} else if n, ok := findRomanSeason(norm); ok {
				season = n
			}
		}
		if year == 0 {
			if m := reYearPar.FindStringSubmatch(norm); m != nil {
				year, _ = strconv.Atoi(strings.Trim(m[0], "()"))
			} else if yearBracketContent(norm) {
				year, _ = strconv.Atoi(norm)
			}
		}
		if media == KindUnknown {
			media, _ = stripMedia(norm)
		}
		if !dirComponentSurvives(norm) {
			continue
		}
		title := cleanDirTitle(norm)
		if title != "" {
			titles = append(titles, title)
		}
	}
	return titles, season, year, media
}

func dirComponentSurvives(comp string) bool {
	trimmed := strings.TrimSpace(comp)
	if trimmed == "" {
		return false
	}
	if genericDirNames[strings.ToLower(trimmed)] {
		return false
	}
	// Pure season folder
	if strings.TrimSpace(reSeasonCN.ReplaceAllString(trimmed, "")) == "" {
		return false
	}
	// Pure year folder OK to skip as title but year already extracted
	if yearBracketContent(trimmed) {
		return false
	}
	// Pure digit directory MUST survive if it's a title like 86
	// (Plan: 不要沿用“纯数字目录一律丢弃”而让作品86消失)
	return true
}

func cleanDirTitle(comp string) string {
	title := reSeasonCN.ReplaceAllString(comp, " ")
	title = stripAllRomans(title)
	_, title = stripMedia(title)
	title = reYearPar.ReplaceAllString(title, " ")
	return cleanTitleSpaces(title)
}
