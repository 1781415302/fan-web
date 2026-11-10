package matching

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// fansubPlaceholder 替换被识别的首块字幕组，保持首块位置语义（契约 4.0 约束 2）。
// 选定文本须令 isClearGroupName 与 looksLikeFansub 恒为真（见 title_clean_test.go）。
const fansubPlaceholder = "Fansub"

// metaPlaceholder 替换现有解析器不认识的组合元数据块，保持后续括号块的位置。
// 选定文本须令 isCompositeMetaBlock 恒为真（归类为噪声，不进标题）。
const metaPlaceholder = "字幕"

// reLeadBracket 匹配串首（允许前导空白）的第一个括号块。
var reLeadBracket = regexp.MustCompile(`^\s*[\[【]([^\]】]*)[\]】]`)

// reBracketLoc 逐个定位括号块（与 reBracket 同形，单独命名避免误改共享变量）。
var reBracketLoc = regexp.MustCompile(`[\[【]([^\]】]*)[\]】]`)

// reOrdinalPart 匹配 Part N / Part II（大小写不敏感，前后须为非字母数字边界）。
var reOrdinalPart = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])part[\s._]*(\d{1,2}|x|ix|viii|vii|vi|iv|v|iii|ii|i)($|[^A-Za-z0-9])`)

// reOrdinalSeasonWord 匹配 First…Tenth Season。
var reOrdinalSeasonWord = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])(first|second|third|fourth|fifth|sixth|seventh|eighth|ninth|tenth)[\s._]+season($|[^A-Za-z0-9])`)

// reFinalSeasonWord 匹配 Final Season（只标记 Final，不猜季号）。
var reFinalSeasonWord = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])final[\s._]+season($|[^A-Za-z0-9])`)

var ordinalWordNum = map[string]int{
	"first": 1, "second": 2, "third": 3, "fourth": 4, "fifth": 5,
	"sixth": 6, "seventh": 7, "eighth": 8, "ninth": 9, "tenth": 10,
}

var partRomanNum = map[string]int{
	"i": 1, "ii": 2, "iii": 3, "iv": 4, "v": 5, "vi": 6, "vii": 7, "viii": 8, "ix": 9, "x": 10,
}

// cjkFansubMetaExtra 现有 cjkMetaTokenList 之外的字幕语言/封装词；与其合并后按长度降序贪心。
var cjkFansubMetaExtra = []string{
	"简繁日双语", "简日双语", "繁日双语", "粤日双语", "中日双语", "简繁双语", "双语字幕",
	"简体中文", "繁体中文", "简日", "繁日", "简中", "繁中", "双语", "中文", "国语", "粤语",
	"硬字幕", "软字幕", "无字幕", "生肉",
}

// latinFansubMeta 组合元数据块中的拉丁词（现有 codec/source/lang/packaging 词表之外）。
var latinFansubMeta = map[string]bool{
	"eng": true, "sub": true, "subs": true, "subbed": true, "dual": true, "audio": true,
	"multisub": true, "multi": true, "multi-sub": true, "web": true, "crc32": true,
	"jpn": true, "chi": true, "hardsub": true, "softsub": true, "dub": true,
}

var fansubMetaTokens = buildFansubMetaTokens()

func buildFansubMetaTokens() []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range append(append([]string{}, cjkMetaTokenList()...), cjkFansubMetaExtra...) {
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	// 长词优先：按字节长度降序贪心，稳定排序保持同长原序
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// extractFansubMeta 预抽取 CJK 字幕组/联署/元数据块。
// 字幕组：仅串首括号块；纯 CJK 或含 &/× 且至少一段为 CJK 的联署，且其后仍有可作标题的内容；
// 命中后替换为占位块 [Fansub]（不删除）。元数据块：现有解析器不认识的组合元数据，替换为 [字幕]。
func extractFansubMeta(stem string, ex *fileExtras) string {
	stem = replaceFansubMetaBlocks(stem, ex)
	if loc := reLeadBracket.FindStringSubmatchIndex(stem); loc != nil {
		content := strings.TrimSpace(stem[loc[2]:loc[3]])
		after := stem[loc[1]:]
		if isLeadingCJKFansub(content) && hasTitleAfter(after) {
			stem = stem[:loc[0]] + "[" + fansubPlaceholder + "]" + after
			if ex != nil {
				ex.Notes = append(ex.Notes, "fansub:"+content)
			}
		}
	}
	return stem
}

// replaceFansubMetaBlocks 把现有解析器不认识的组合元数据块替换为占位块。
func replaceFansubMetaBlocks(stem string, ex *fileExtras) string {
	locs := reBracketLoc.FindAllStringSubmatchIndex(stem, -1)
	if len(locs) == 0 {
		return stem
	}
	var b strings.Builder
	prev := 0
	for _, loc := range locs {
		content := strings.TrimSpace(stem[loc[2]:loc[3]])
		if content == "" || isCompositeMetaBlock(content) || !isFansubMetaBlock(content) {
			continue
		}
		b.WriteString(stem[prev:loc[0]])
		b.WriteString("[" + metaPlaceholder + "]")
		prev = loc[1]
		if ex != nil {
			ex.Notes = append(ex.Notes, "meta:"+content)
		}
	}
	if prev == 0 {
		return stem
	}
	b.WriteString(stem[prev:])
	return b.String()
}

// isFansubMetaBlock 块内全部字段都是字幕语言/封装/规格/CRC 词；单个拉丁字段交给现有分类。
func isFansubMetaBlock(b string) bool {
	fields := strings.FieldsFunc(b, func(r rune) bool {
		return unicode.IsSpace(r) || r == '/' || r == ',' || r == '+' || r == '&' || r == '_' || r == '|' || r == '·'
	})
	if len(fields) == 0 {
		return false
	}
	hasCJKField := false
	for _, f := range fields {
		if hasCJK(f) {
			if hasLatinLetter(f) || !greedyMatchTokens(f, fansubMetaTokens) {
				return false
			}
			hasCJKField = true
			continue
		}
		if !isLatinMetaField(f) {
			return false
		}
	}
	return len(fields) >= 2 || hasCJKField
}

func isLatinMetaField(f string) bool {
	low := strings.ToLower(strings.Trim(f, "-.()"))
	if low == "" {
		return false
	}
	if latinFansubMeta[low] || codecTokens[low] || sourceTokens[low] || langTokens[low] || packagingTokens[low] {
		return true
	}
	if reRes.MatchString(low) || reSceneResTok.MatchString(low) {
		return true
	}
	return reCRC.MatchString(f)
}

// greedyMatchTokens 用长度降序词表贪心切分；须完整覆盖。
func greedyMatchTokens(s string, tokens []string) bool {
	remain := s
	matched := 0
	for remain != "" {
		progress := false
		for _, tok := range tokens {
			if strings.HasPrefix(remain, tok) {
				remain = remain[len(tok):]
				matched++
				progress = true
				break
			}
		}
		if !progress {
			return false
		}
	}
	return matched > 0
}

// isLeadingCJKFansub 首块字幕组启发：纯 CJK 组名，或含 &/× 且至少一段为 CJK 组名的联署。
func isLeadingCJKFansub(b string) bool {
	b = strings.TrimSpace(b)
	if b == "" {
		return false
	}
	if isPureCJKGroupName(b) {
		return true
	}
	if !strings.ContainsAny(b, "&×") {
		return false
	}
	parts := strings.FieldsFunc(b, func(r rune) bool { return r == '&' || r == '×' })
	if len(parts) < 2 {
		return false
	}
	cjkPart := false
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return false
		}
		if isPureCJKGroupName(p) {
			cjkPart = true
			continue
		}
		if hasCJK(p) || !isLatinGroupPart(p) {
			return false
		}
	}
	return cjkPart
}

// isPureCJKGroupName 纯汉字/假名、长度适中，且不是季号、类别、媒体或元数据词。
func isPureCJKGroupName(b string) bool {
	n := utf8.RuneCountInString(b)
	if n < 2 || n > 16 {
		return false
	}
	for _, r := range b {
		if !(unicode.Is(unicode.Han, r) || unicode.In(r, unicode.Hiragana, unicode.Katakana) || r == 'ー') {
			return false
		}
	}
	if categoryExact[b] || mediaBracketOnly(b) || isCompositeMetaBlock(b) || greedyMatchTokens(b, fansubMetaTokens) {
		return false
	}
	if reSeasonCN.MatchString(b) || reEpCN.MatchString(b) {
		return false
	}
	if mk, _ := stripMedia(b); mk != KindUnknown {
		return false
	}
	return true
}

// isLatinGroupPart 联署中的拉丁组名段：沿用 looksLikeLeadingFansub 的字符与长度约束。
func isLatinGroupPart(p string) bool {
	if looksLikeKnownTitleForm(p) || hasTitleDigitIdentity(p) {
		return false
	}
	return looksLikeFansub(p) || looksLikeLeadingFansub(p)
}

// hasTitleAfter 首块之后是否仍有可作标题的块或括号外文字（单块/首块即唯一标题时为假）。
func hasTitleAfter(after string) bool {
	blocks, rest := splitBracketBlocks(after)
	for _, raw := range blocks {
		if isTitleCandidateText(strings.TrimSpace(raw), true) {
			return true
		}
	}
	return isTitleCandidateText(rest, false)
}

// isTitleCandidateText 去掉集号/季号/规格/元数据字段后是否仍有字母或 CJK。
func isTitleCandidateText(s string, bracket bool) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if bracket {
		if isCompositeMetaBlock(s) || isFansubMetaBlock(s) || categoryExact[s] || categoryExact[strings.ToLower(s)] || mediaBracketOnly(s) {
			return false
		}
		if yearBracketContent(s) || reCRC.MatchString(s) || reRes.MatchString(strings.ReplaceAll(s, " ", "")) || reEpBracket.MatchString(s) {
			return false
		}
		low := strings.ToLower(s)
		if codecTokens[low] || sourceTokens[low] || langTokens[low] || langTokens[s] || hasAnyToken(low, codecTokens) || hasAnyToken(low, sourceTokens) {
			return false
		}
		if mk, restMedia := stripMedia(s); mk != KindUnknown && strings.TrimSpace(restMedia) == "" {
			return false
		}
	}
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r))
	})
	for _, f := range fields {
		if isNumberingField(f) {
			continue
		}
		low := strings.ToLower(f)
		if reRes.MatchString(low) || reSceneResTok.MatchString(low) || codecTokens[low] || sourceTokens[low] || langTokens[low] || latinFansubMeta[low] || packagingTokens[low] {
			continue
		}
		if hasCJK(f) && greedyMatchTokens(f, fansubMetaTokens) {
			continue
		}
		if hasCJK(f) || hasLatinLetter(f) {
			return true
		}
	}
	return false
}

// numberingLatinResidue 删去数字后只剩集/季/修订标记的拉丁字段（S04E19、4th、05v2、EP、END）。
var numberingLatinResidue = map[string]bool{
	"": true, "s": true, "e": true, "se": true, "ep": true, "v": true, "ev": true, "sev": true, "epv": true,
	"st": true, "nd": true, "rd": true, "th": true, "end": true, "fin": true,
	"season": true, "episode": true, "sp": true,
}

func isNumberingField(f string) bool {
	if hasCJK(f) {
		rest := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' || strings.ContainsRune("第话話集季期总總完一二三四五六七八九十百", r) {
				return -1
			}
			return r
		}, f)
		return rest == ""
	}
	rest := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return -1
		}
		return unicode.ToLower(r)
	}, f)
	return numberingLatinResidue[rest]
}

// extractOrdinalWords 预抽取 Part N、Second/Third/Final Season 等序数词。
// Part N / 序数词 Season 从串中移除并写入 extras；Final Season 只标记 Final、不猜季号、不移除文字。
func extractOrdinalWords(stem string, ex *fileExtras) string {
	if m := reOrdinalPart.FindStringSubmatchIndex(stem); m != nil {
		numText := strings.ToLower(stem[m[4]:m[5]])
		n := partRomanNum[numText]
		if n == 0 {
			n = parseChineseOrASCIINum(numText)
		}
		if n > 0 {
			if ex != nil && ex.Part == 0 {
				ex.Part = n
			}
			stem = stem[:m[3]] + " " + stem[m[6]:]
		}
	}
	if m := reOrdinalSeasonWord.FindStringSubmatchIndex(stem); m != nil {
		n := ordinalWordNum[strings.ToLower(stem[m[4]:m[5]])]
		if n > 0 && ex != nil {
			switch {
			case ex.Season == 0 || ex.Season == n:
				ex.Season = n
				stem = stem[:m[3]] + " " + stem[m[6]:]
			default:
				// 与前序 pass 已抽出的季号不一致：不覆盖、不移除原文，仅记诊断（保守）
				ex.Notes = append(ex.Notes, "ordinal-season-conflict")
			}
		}
	}
	if reFinalSeasonWord.MatchString(stem) && ex != nil {
		ex.Final = true
	}
	return stem
}
