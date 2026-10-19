package matching

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

// 初始评分与接受阈值（§4.2C 固定规则；调参属 S6，不得为单例改数）。
const (
	nameScoreStrictEqual   = 1.0
	nameScoreNormalEqual   = 0.98
	nameScoreCatalogEquiv  = 0.97
	nameScoreWeakCap       = 0.90
	nameScoreAcceptMin     = 0.88
	scoreAcceptMin         = 0.92
	scoreGapMin            = 0.10
	structBonusSeasonPart  = 0.03
	structBonusYearSame    = 0.02
	structBonusChapterConf = 0.02
	structPenaltyYearDiff1 = 0.02
	diceWeight             = 0.6
	lcsWeight              = 0.4
	shortLatinMaxRunes     = 4
	shortCJKMaxRunes       = 2
	fragmentConflictMin    = 0.88
	confirmNameScoreMin    = 0.70
	nameScoreProbeMin      = 0.80 // Session 探测章节用；本包仅集中声明
)

type nameHitKind int

const (
	hitNone nameHitKind = iota
	hitStrict
	hitNormal
	hitCatalog
	hitFuzzy
	hitWeak // 副标题包含 / Loose 单独命中
)

type nameHit struct {
	score    float64
	kind     nameHitKind
	support  string
	titleIdx int // 查询标题下标；-1 表示无
}

// Rank 对 subjects 评分并稳定排序。chapters 按 subject ID 读取；nil/缺项=Unchecked。
func Rank(q Query, subjects []Subject, catalog Catalog, chapters map[int]ChapterDecision) []Candidate {
	if len(subjects) == 0 {
		return []Candidate{}
	}
	idents := scoreIdentityTitles(q, catalog)
	out := make([]Candidate, 0, len(subjects))

	// 每条查询标题对每个 subject 的最佳身份分（片段冲突检测）
	fragBest := make([]struct {
		id    int
		score float64
	}, len(idents))

	type scored struct {
		c    Candidate
		hits []nameHit // 与 idents 对齐
	}
	tmp := make([]scored, 0, len(subjects))

	for _, sub := range subjects {
		c, hits := scoreSubject(q, sub, catalog, idents, chapterStateOf(chapters, sub.ID))
		tmp = append(tmp, scored{c: c, hits: hits})
		for i, h := range hits {
			if h.score >= fragBest[i].score {
				fragBest[i].score = h.score
				fragBest[i].id = sub.ID
			}
		}
	}

	// 并列语言片段：各自 ≥0.88 支持不同 subject → 相关候选 Missing
	conflictIDs := fragmentConflictIDs(fragBest)
	for i := range tmp {
		if conflictIDs[tmp[i].c.Subject.ID] {
			tmp[i].c.Missing = appendUniqueString(tmp[i].c.Missing, "并列语言片段支持不同作品")
		}
		out = append(out, tmp[i].c)
	}

	sort.SliceStable(out, func(i, j int) bool {
		bi, bj := len(out[i].Blockers) > 0, len(out[j].Blockers) > 0
		if bi != bj {
			return !bi && bj // 无阻断者优先
		}
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].NameScore != out[j].NameScore {
			return out[i].NameScore > out[j].NameScore
		}
		return out[i].Subject.ID < out[j].Subject.ID
	})
	return out
}

func chapterStateOf(chapters map[int]ChapterDecision, id int) ChapterDecision {
	if chapters == nil {
		return ChapterDecision{State: ChapterUnchecked, Rejected: map[string]string{}}
	}
	d, ok := chapters[id]
	if !ok {
		return ChapterDecision{State: ChapterUnchecked, Rejected: map[string]string{}}
	}
	return d
}

func fragmentConflictIDs(fragBest []struct {
	id    int
	score float64
}) map[int]bool {
	strong := make(map[int]bool)
	var ids []int
	for _, f := range fragBest {
		if f.score >= fragmentConflictMin && f.id != 0 {
			if !strong[f.id] {
				ids = append(ids, f.id)
			}
			strong[f.id] = true
		}
	}
	if len(ids) <= 1 {
		return nil
	}
	return strong
}

type identityTitle struct {
	text   string
	source Source
	facts  TitleFacts
	forms  TextForms
}

func scoreIdentityTitles(q Query, catalog Catalog) []identityTitle {
	var raw []Title
	if q.HasFileTitle {
		for _, t := range q.Titles {
			if t.Source == SourceDirectory {
				continue
			}
			if t.Source == SourceCatalog {
				// 已是扩展结果，直接可用
				raw = append(raw, t)
				continue
			}
			raw = append(raw, t)
			for _, et := range catalog.Expand(t.Text) {
				raw = append(raw, et)
			}
		}
	} else {
		for _, t := range q.Titles {
			if t.Source == SourceFile {
				continue
			}
			raw = append(raw, t)
			for _, et := range catalog.Expand(t.Text) {
				raw = append(raw, et)
			}
		}
		// HasFileTitle=false 且未标 SourceDirectory 时，全部视为目录线索
		if len(raw) == 0 {
			for _, t := range q.Titles {
				raw = append(raw, t)
				for _, et := range catalog.Expand(t.Text) {
					raw = append(raw, et)
				}
			}
		}
	}

	out := make([]identityTitle, 0, len(raw))
	seen := map[string]struct{}{}
	for _, t := range raw {
		text := strings.TrimSpace(t.Text)
		if text == "" {
			continue
		}
		key := TitleKey(text)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		facts := AnalyzeTitle(text)
		out = append(out, identityTitle{
			text:   text,
			source: t.Source,
			facts:  facts,
			forms:  Normalize(text),
		})
	}
	return out
}

func scoreSubject(q Query, sub Subject, catalog Catalog, idents []identityTitle, ch ChapterDecision) (Candidate, []nameHit) {
	c := Candidate{
		Subject:  sub,
		Support:  nil,
		Blockers: nil,
		Missing:  nil,
	}
	hits := make([]nameHit, len(idents))

	subNames := subjectNameList(sub)
	best := nameHit{score: 0, kind: hitNone, titleIdx: -1}

	for i, id := range idents {
		h := bestNameAgainst(id, subNames, catalog)
		h.titleIdx = i
		hits[i] = h
		if h.score > best.score || (h.score == best.score && h.kind < best.kind) {
			best = h
		}
	}

	c.NameScore = clamp01(best.score)
	if best.support != "" {
		c.Support = append(c.Support, best.support)
	}

	// 短标题：仅严格/常规/catalog 可自动接受
	if best.kind == hitFuzzy || best.kind == hitWeak {
		if isShortTitle(bestBase(idents, best)) {
			c.Missing = appendUniqueString(c.Missing, "短标题仅允许精确或catalog等价")
		}
	}
	if best.kind == hitWeak {
		c.Missing = appendUniqueString(c.Missing, "弱形态身份支持不能自动接受")
	}

	// 查询冲突 → 阻断
	for _, conf := range q.Conflicts {
		c.Blockers = appendUniqueString(c.Blockers, conf)
	}

	// 结构：季/Part/Final/媒体/年份
	applyStructure(&c, q, sub)

	// 区分符冲突
	applyDiscriminatorConflicts(&c, idents, subNames)

	// 章节
	switch ch.State {
	case ChapterConflict:
		c.Blockers = appendUniqueString(c.Blockers, "章节解释冲突")
	case ChapterConfirmed:
		c.Support = appendUniqueString(c.Support, "章节已确认")
	case ChapterCountFit:
		c.Support = appendUniqueString(c.Support, "章节按话数拟合")
	}

	// Score = NameScore + 结构支持
	score := c.NameScore
	score += structureBonus(q, sub, ch)
	c.Score = clamp01(score)

	return c, hits
}

func bestBase(idents []identityTitle, best nameHit) string {
	if best.titleIdx >= 0 && best.titleIdx < len(idents) {
		b := strings.TrimSpace(idents[best.titleIdx].facts.Base)
		if b != "" {
			return b
		}
		return idents[best.titleIdx].text
	}
	return ""
}

func subjectNameList(sub Subject) []identityTitle {
	raw := make([]string, 0, 2+len(sub.Aliases))
	if strings.TrimSpace(sub.Name) != "" {
		raw = append(raw, sub.Name)
	}
	if strings.TrimSpace(sub.NameCn) != "" {
		raw = append(raw, sub.NameCn)
	}
	raw = append(raw, sub.Aliases...)
	out := make([]identityTitle, 0, len(raw))
	seen := map[string]struct{}{}
	for _, text := range raw {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		key := TitleKey(text)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, identityTitle{
			text:   text,
			source: SourceSubject,
			facts:  AnalyzeTitle(text),
			forms:  Normalize(text),
		})
	}
	return out
}

func bestNameAgainst(query identityTitle, subjects []identityTitle, catalog Catalog) nameHit {
	best := nameHit{kind: hitNone}
	for _, sub := range subjects {
		h := compareNames(query, sub, catalog)
		if h.score > best.score {
			best = h
		} else if h.score == best.score && h.kind != hitNone && (best.kind == hitNone || h.kind < best.kind) {
			best = h
		}
	}
	return best
}

func compareNames(q, s identityTitle, catalog Catalog) nameHit {
	// 1. 严格全名
	if q.forms.Strict != "" && q.forms.Strict == s.forms.Strict {
		return nameHit{score: nameScoreStrictEqual, kind: hitStrict, support: "严格全名相等"}
	}
	// 2. 常规全名
	if q.forms.Normal != "" && q.forms.Normal == s.forms.Normal {
		return nameHit{score: nameScoreNormalEqual, kind: hitNormal, support: "常规全名相等"}
	}

	qBase := strings.TrimSpace(q.facts.Base)
	sBase := strings.TrimSpace(s.facts.Base)
	if qBase == "" {
		qBase = q.text
	}
	if sBase == "" {
		sBase = s.text
	}
	qBaseForms := Normalize(qBase)
	sBaseForms := Normalize(sBase)

	// base 级严格/常规（剥季后的全名）
	if qBaseForms.Strict != "" && qBaseForms.Strict == sBaseForms.Strict {
		return nameHit{score: nameScoreStrictEqual, kind: hitStrict, support: "严格base相等"}
	}
	if qBaseForms.Normal != "" && qBaseForms.Normal == sBaseForms.Normal {
		return nameHit{score: nameScoreNormalEqual, kind: hitNormal, support: "常规base相等"}
	}

	// 3. catalog 精确系列 base 等价
	if catalogBaseEquiv(qBase, sBase, catalog) || catalogBaseEquiv(q.text, s.text, catalog) {
		return nameHit{score: nameScoreCatalogEquiv, kind: hitCatalog, support: "catalog系列base等价"}
	}

	// 4. Loose 单独命中
	if qBaseForms.Loose != "" && qBaseForms.Loose == sBaseForms.Loose {
		return nameHit{score: nameScoreWeakCap, kind: hitWeak, support: "Loose命中"}
	}
	if q.forms.Loose != "" && q.forms.Loose == s.forms.Loose {
		return nameHit{score: nameScoreWeakCap, kind: hitWeak, support: "Loose全名命中"}
	}

	// 5. 模糊：Dice + LCS；副标题/前缀包含封顶
	fuzzy := diceWeight*multisetDiceBigram(qBaseForms.Normal, sBaseForms.Normal) +
		lcsWeight*lcsRatio(qBaseForms.Normal, sBaseForms.Normal)
	kind := hitFuzzy
	support := "模糊名称"
	if isPrefixOrSubtitleContain(qBaseForms.Normal, sBaseForms.Normal) {
		if fuzzy > nameScoreWeakCap {
			fuzzy = nameScoreWeakCap
		}
		kind = hitWeak
		support = "副标题或前缀包含"
	}
	if fuzzy <= 0 {
		return nameHit{kind: hitNone}
	}
	return nameHit{score: fuzzy, kind: kind, support: support}
}

func catalogBaseEquiv(a, b string, catalog Catalog) bool {
	if catalog.Empty() {
		return false
	}
	aBase := strings.TrimSpace(AnalyzeTitle(a).Base)
	bBase := strings.TrimSpace(AnalyzeTitle(b).Base)
	if aBase == "" {
		aBase = strings.TrimSpace(a)
	}
	if bBase == "" {
		bBase = strings.TrimSpace(b)
	}
	ai, aok := catalog.lookupBase(aBase)
	bi, bok := catalog.lookupBase(bBase)
	if aok && bok && ai == bi {
		return true
	}
	// 一侧是 family 内别名、另一侧直接命中
	if aok {
		for _, t := range familyTitles(catalog.families[ai]) {
			if TitleKey(t) == TitleKey(bBase) || TitleKey(t) == TitleKey(b) {
				return true
			}
		}
	}
	if bok {
		for _, t := range familyTitles(catalog.families[bi]) {
			if TitleKey(t) == TitleKey(aBase) || TitleKey(t) == TitleKey(a) {
				return true
			}
		}
	}
	return false
}

func applyStructure(c *Candidate, q Query, sub Subject) {
	subOrd := subjectOrdinal(sub)
	subMedia := sub.Media

	// 季
	if q.Ordinal.Season > 0 {
		if subOrd.Season > 0 {
			if q.Ordinal.Season != subOrd.Season {
				c.Blockers = appendUniqueString(c.Blockers,
					fmt.Sprintf("季号冲突：查询%d vs 候选%d", q.Ordinal.Season, subOrd.Season))
			} else {
				c.Support = appendUniqueString(c.Support, "季号相容")
			}
		} else {
			// 无标记 ≠ 第一季
			c.Missing = appendUniqueString(c.Missing, "候选无法解释查询季号")
		}
	}
	// Part
	if q.Ordinal.Part > 0 {
		if subOrd.Part > 0 && q.Ordinal.Part != subOrd.Part {
			c.Blockers = appendUniqueString(c.Blockers,
				fmt.Sprintf("Part冲突：查询%d vs 候选%d", q.Ordinal.Part, subOrd.Part))
		} else if subOrd.Part == 0 && q.Ordinal.Part > 0 {
			c.Missing = appendUniqueString(c.Missing, "候选无法解释Part")
		} else if subOrd.Part == q.Ordinal.Part && q.Ordinal.Part > 0 {
			c.Support = appendUniqueString(c.Support, "Part相容")
		}
	}
	// Final
	if q.Ordinal.Final {
		if !subOrd.Final {
			// 候选名称未标 Final：缺证据而非硬冲突
			c.Missing = appendUniqueString(c.Missing, "候选无法解释Final")
		} else {
			c.Support = appendUniqueString(c.Support, "Final相容")
		}
	} else if subOrd.Final && (q.Ordinal.Season > 0 || q.Media == KindSeries) {
		// 查询非 Final 却命中 Final 条目：明确结构不符
		c.Blockers = appendUniqueString(c.Blockers, "候选为Final与查询结构不符")
	}

	// 媒体：TV/WEB 同属 Series，不互相否定；空串视同 unknown
	qMedia := q.Media
	if qMedia == "" {
		qMedia = KindUnknown
	}
	if subMedia == "" {
		subMedia = KindUnknown
	}
	if qMedia != KindUnknown && subMedia != KindUnknown {
		if !mediaCompatible(qMedia, subMedia) {
			c.Blockers = appendUniqueString(c.Blockers,
				fmt.Sprintf("类型冲突：查询%s vs 候选%s", qMedia, subMedia))
		} else if qMedia == subMedia || (qMedia == KindSeries && subMedia == KindSeries) {
			c.Support = appendUniqueString(c.Support, "类型相容")
		}
	}

	// 年份
	if q.Year > 0 && sub.Year > 0 {
		diff := q.Year - sub.Year
		if diff < 0 {
			diff = -diff
		}
		if diff >= 2 {
			c.Blockers = appendUniqueString(c.Blockers,
				fmt.Sprintf("年份冲突：查询%d vs 候选%d", q.Year, sub.Year))
		} else if diff == 0 {
			c.Support = appendUniqueString(c.Support, "年份相同")
		} else {
			c.Support = appendUniqueString(c.Support, "年份相差1")
		}
	}
}

func structureBonus(q Query, sub Subject, ch ChapterDecision) float64 {
	var bonus float64
	subOrd := subjectOrdinal(sub)
	seasonPart := 0.0
	if q.Ordinal.Season > 0 && subOrd.Season > 0 && q.Ordinal.Season == subOrd.Season {
		seasonPart += 0.02
	}
	if q.Ordinal.Part > 0 && subOrd.Part > 0 && q.Ordinal.Part == subOrd.Part {
		seasonPart += 0.01
	}
	if q.Ordinal.Final && subOrd.Final {
		seasonPart += 0.01
	}
	if seasonPart > structBonusSeasonPart {
		seasonPart = structBonusSeasonPart
	}
	bonus += seasonPart

	if q.Year > 0 && sub.Year > 0 {
		diff := q.Year - sub.Year
		if diff < 0 {
			diff = -diff
		}
		if diff == 0 {
			bonus += structBonusYearSame
		} else if diff == 1 {
			bonus -= structPenaltyYearDiff1
		}
	}
	if ch.State == ChapterConfirmed {
		bonus += structBonusChapterConf
	}
	return bonus
}

func subjectOrdinal(sub Subject) Ordinal {
	var seasons, parts []int
	var finals int
	add := func(text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		f := AnalyzeTitle(text)
		if f.Ordinal.Season > 0 {
			seasons = append(seasons, f.Ordinal.Season)
		}
		if f.Ordinal.Part > 0 {
			parts = append(parts, f.Ordinal.Part)
		}
		if f.Ordinal.Final {
			finals++
		}
	}
	add(sub.Name)
	add(sub.NameCn)
	for _, a := range sub.Aliases {
		add(a)
	}
	var ord Ordinal
	su := uniquePositive(seasons)
	if len(su) == 1 {
		ord.Season = su[0]
	}
	// 多值歧义：保留未知（0），不任选
	pu := uniquePositive(parts)
	if len(pu) == 1 {
		ord.Part = pu[0]
	}
	if finals > 0 && finals == 1+len(sub.Aliases) || (finals > 0 && len(su) <= 1) {
		// 任一名称标 Final 即认为候选带 Final 标记
		ord.Final = finals > 0
	}
	return ord
}

func mediaCompatible(a, b MediaKind) bool {
	if a == b {
		return true
	}
	// TV/WEB 均 KindSeries
	if a == KindSeries && b == KindSeries {
		return true
	}
	return false
}

func applyDiscriminatorConflicts(c *Candidate, idents []identityTitle, subNames []identityTitle) {
	// 收集查询侧与候选侧区分符；数字/字母冲突 → Blockers
	qDisc := map[string]struct{}{}
	for _, id := range idents {
		if id.source == SourceDirectory && c.Subject.ID != 0 {
			// 目录区分符不单独制造身份阻断（HasFileTitle 时）
		}
		for _, d := range id.facts.Discriminators {
			qDisc[normalizeDisc(d)] = struct{}{}
		}
	}
	sDisc := map[string]struct{}{}
	for _, sn := range subNames {
		for _, d := range sn.facts.Discriminators {
			sDisc[normalizeDisc(d)] = struct{}{}
		}
	}
	if len(qDisc) == 0 || len(sDisc) == 0 {
		return
	}
	// bang 冲突
	if _, q1 := qDisc["!"]; q1 {
		if _, s2 := sDisc["!!"]; s2 {
			c.Blockers = appendUniqueString(c.Blockers, "区分符冲突：! vs !!")
		}
	}
	if _, q2 := qDisc["!!"]; q2 {
		if _, s1 := sDisc["!"]; s1 {
			if _, s2 := sDisc["!!"]; !s2 {
				c.Blockers = appendUniqueString(c.Blockers, "区分符冲突：!! vs !")
			}
		}
	}
	// 单字母季标
	qLetters := letterDiscs(qDisc)
	sLetters := letterDiscs(sDisc)
	if len(qLetters) > 0 && len(sLetters) > 0 {
		for ql := range qLetters {
			for sl := range sLetters {
				if ql != sl {
					c.Blockers = appendUniqueString(c.Blockers,
						fmt.Sprintf("区分符冲突：%s vs %s", ql, sl))
				}
			}
		}
	}
}

func normalizeDisc(d string) string {
	return strings.ToLower(strings.TrimSpace(d))
}

func letterDiscs(m map[string]struct{}) map[string]struct{} {
	out := map[string]struct{}{}
	for d := range m {
		if utf8.RuneCountInString(d) == 1 {
			r := []rune(d)[0]
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				out[strings.ToLower(d)] = struct{}{}
			}
		}
	}
	return out
}

func isShortTitle(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	n := utf8.RuneCountInString(text)
	if hasCJK(text) {
		return n <= shortCJKMaxRunes
	}
	// 纯拉丁：去空白后计数
	compact := strings.ReplaceAll(TitleKey(text), " ", "")
	return utf8.RuneCountInString(compact) <= shortLatinMaxRunes
}

func isPrefixOrSubtitleContain(a, b string) bool {
	if a == "" || b == "" || a == b {
		return false
	}
	shorter, longer := a, b
	if len(a) > len(b) {
		shorter, longer = b, a
	}
	if strings.HasPrefix(longer, shorter) {
		return true
	}
	// 副标题：冒号/破折号后
	for _, sep := range []string{":", "：", " - ", "-"} {
		if i := strings.Index(longer, sep); i > 0 {
			main := strings.TrimSpace(longer[:i])
			if main == shorter {
				return true
			}
		}
	}
	return strings.Contains(longer, shorter) && utf8.RuneCountInString(shorter)*2 >= utf8.RuneCountInString(longer)
}

func multisetDiceBigram(a, b string) float64 {
	ra := []rune(a)
	rb := []rune(b)
	if len(ra) < 2 || len(rb) < 2 {
		if len(ra) > 0 && string(ra) == string(rb) {
			return 1
		}
		return 0
	}
	ba := countBigramsScore(ra)
	bb := countBigramsScore(rb)
	inter := 0
	for g, ca := range ba {
		if cb, ok := bb[g]; ok {
			if ca < cb {
				inter += ca
			} else {
				inter += cb
			}
		}
	}
	den := float64(len(ra) - 1 + len(rb) - 1)
	if den == 0 {
		return 0
	}
	return 2 * float64(inter) / den
}

func countBigramsScore(rs []rune) map[string]int {
	counts := make(map[string]int, len(rs)-1)
	for i := 0; i < len(rs)-1; i++ {
		counts[string(rs[i:i+2])]++
	}
	return counts
}

func lcsRatio(a, b string) float64 {
	ra := []rune(a)
	rb := []rune(b)
	if len(ra) == 0 || len(rb) == 0 {
		return 0
	}
	l := lcsLen(ra, rb)
	return 2 * float64(l) / float64(len(ra)+len(rb))
}

func lcsLen(a, b []rune) int {
	n, m := len(a), len(b)
	if n == 0 || m == 0 {
		return 0
	}
	// 滚动数组，Go 1.21 兼容
	prev := make([]int, m+1)
	cur := make([]int, m+1)
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if a[i-1] == b[j-1] {
				cur[j] = prev[j-1] + 1
			} else if prev[j] >= cur[j-1] {
				cur[j] = prev[j]
			} else {
				cur[j] = cur[j-1]
			}
		}
		prev, cur = cur, prev
		for j := range cur {
			cur[j] = 0
		}
	}
	return prev[m]
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	// 避免 -0 / NaN
	if math.IsNaN(v) {
		return 0
	}
	return v
}

func appendUniqueString(xs []string, s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return xs
	}
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}

// 供测试断言阈值集中声明。
func scoreThresholds() (nameMin, scoreMin, gapMin float64) {
	return nameScoreAcceptMin, scoreAcceptMin, scoreGapMin
}
