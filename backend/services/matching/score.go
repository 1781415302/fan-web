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

type nameHit struct {
	score       float64
	kind        NameMatchKind
	support     string
	titleIdx    int // 查询标题下标；-1 表示无
	queryText   string
	subjectText string
	querySource Source
	subjectRole SubjectNameRole
}

// Rank 对 subjects 评分并稳定排序。chapters 按 subject ID 读取；nil/缺项=Unchecked。
// 次序：名称证据 → 硬结构/区分符 → 章节状态 → 单候选策略 → Missing → 并列标题冲突 → 参考项偏好 → 稳定排序。
func Rank(q Query, subjects []Subject, catalog Catalog, chapters map[int]ChapterDecision) []Candidate {
	if len(subjects) == 0 {
		return []Candidate{}
	}
	idents := scoreIdentityTitles(q, catalog)

	type scored struct {
		c    Candidate
		hits []nameHit
		ch   ChapterDecision
	}
	tmp := make([]scored, 0, len(subjects))

	// 每条查询标题对每个 subject 的最佳身份分（片段冲突检测）
	fragBest := make([]struct {
		id    int
		score float64
	}, len(idents))

	for _, sub := range subjects {
		ch := chapterStateOf(chapters, sub.ID)
		c, hits := scoreSubject(q, sub, catalog, idents, ch)
		tmp = append(tmp, scored{c: c, hits: hits, ch: ch})
		for i, h := range hits {
			if h.score >= fragBest[i].score {
				fragBest[i].score = h.score
				fragBest[i].id = sub.ID
			}
		}
	}

	conflictIDs := fragmentConflictIDs(fragBest)

	out := make([]Candidate, 0, len(tmp))
	for i := range tmp {
		c := tmp[i].c
		frag := conflictIDs[c.Subject.ID]
		pol := evaluateCandidatePolicy(q, c.Subject, c.NameEvidence, tmp[i].ch)
		mergePolicyIntoCandidate(&c, pol, q, frag, tmp[i].ch)
		if frag {
			c.Missing = appendUniqueString(c.Missing, "并列语言片段支持不同作品")
		}
		out = append(out, c)
	}

	out = applyVariantPreference(q, out)

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
		return ChapterDecision{State: ChapterUnchecked, Rejected: map[string]string{}, Coverage: emptyChapterCoverage(nil)}
	}
	d, ok := chapters[id]
	if !ok {
		return ChapterDecision{State: ChapterUnchecked, Rejected: map[string]string{}, Coverage: emptyChapterCoverage(nil)}
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
	role   SubjectNameRole // 仅条目侧有意义
}

func scoreIdentityTitles(q Query, catalog Catalog) []identityTitle {
	var raw []Title
	if q.HasFileTitle {
		for _, t := range q.Titles {
			if t.Source == SourceDirectory {
				continue
			}
			if t.Source == SourceCatalog {
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
	best := nameHit{score: 0, kind: NameNone, titleIdx: -1}

	for i, id := range idents {
		h := bestNameAgainst(id, subNames, catalog, q.HasFileTitle)
		h.titleIdx = i
		hits[i] = h
		if betterNameHit(h, best, q.HasFileTitle) {
			best = h
		}
	}

	c.NameScore = clamp01(best.score)
	if best.support != "" {
		c.Support = append(c.Support, best.support)
	}
	c.NameEvidence = nameEvidenceFromHit(best, idents, q)
	c.NameEvidence.DirectSupport = computeDirectSupport(idents, subNames, catalog, q)

	// 短标题检查在 mergePolicy 中按 Kind 处理；此处仅结构/区分符/章节

	for _, conf := range q.Conflicts {
		c.Blockers = appendUniqueString(c.Blockers, conf)
	}

	applyStructure(&c, q, sub)
	applyDiscriminatorConflicts(&c, idents, subNames)

	switch ch.State {
	case ChapterConflict:
		c.Blockers = appendUniqueString(c.Blockers, "章节解释冲突")
	case ChapterConfirmed:
		c.Support = appendUniqueString(c.Support, "章节已确认")
	case ChapterCountFit:
		c.Support = appendUniqueString(c.Support, "章节按话数拟合")
	}

	score := c.NameScore
	score += structureBonus(q, sub, ch)
	c.Score = clamp01(score)

	return c, hits
}

func betterNameHit(h, best nameHit, hasFileTitle bool) bool {
	if h.kind == NameNone {
		return false
	}
	if best.kind == NameNone {
		return true
	}
	if h.score != best.score {
		return h.score > best.score
	}
	hr, br := nameKindRank(h.kind), nameKindRank(best.kind)
	if hr != br {
		return hr < br
	}
	hs, bs := sourcePreferRank(h.querySource, hasFileTitle), sourcePreferRank(best.querySource, hasFileTitle)
	if hs != bs {
		return hs < bs
	}
	if subjectRoleRank(h.subjectRole) != subjectRoleRank(best.subjectRole) {
		return subjectRoleRank(h.subjectRole) < subjectRoleRank(best.subjectRole)
	}
	if h.titleIdx >= 0 && best.titleIdx >= 0 && h.titleIdx != best.titleIdx {
		return h.titleIdx < best.titleIdx
	}
	return false
}

func nameEvidenceFromHit(best nameHit, idents []identityTitle, q Query) NameEvidence {
	if best.kind == NameNone {
		return NameEvidence{}
	}
	ev := NameEvidence{
		Kind:        best.kind,
		QueryText:   best.queryText,
		SubjectText: best.subjectText,
		QuerySource: best.querySource,
		SubjectRole: best.subjectRole,
	}
	if ev.QueryText == "" && best.titleIdx >= 0 && best.titleIdx < len(idents) {
		ev.QueryText = idents[best.titleIdx].text
		ev.QuerySource = idents[best.titleIdx].source
	}
	_ = q
	return ev
}

func computeDirectSupport(idents []identityTitle, subNames []identityTitle, catalog Catalog, q Query) bool {
	for _, id := range idents {
		if id.source == SourceCatalog {
			continue
		}
		if q.HasFileTitle && id.source != SourceFile {
			continue
		}
		if !q.HasFileTitle && id.source != SourceDirectory && id.source != SourceFile {
			continue
		}
		for _, sn := range subNames {
			h := compareNames(id, sn, catalog)
			if nameKindStrongIdentity(h.kind) {
				return true
			}
		}
	}
	return false
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
	type named struct {
		text string
		role SubjectNameRole
	}
	raw := make([]named, 0, 2+len(sub.Aliases))
	if strings.TrimSpace(sub.Name) != "" {
		raw = append(raw, named{sub.Name, SubjectPrimary})
	}
	if strings.TrimSpace(sub.NameCn) != "" {
		raw = append(raw, named{sub.NameCn, SubjectPrimary})
	}
	for _, a := range sub.Aliases {
		raw = append(raw, named{a, SubjectAlias})
	}
	out := make([]identityTitle, 0, len(raw))
	seen := map[string]struct{}{}
	for _, n := range raw {
		text := strings.TrimSpace(n.text)
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
			role:   n.role,
		})
	}
	return out
}

func bestNameAgainst(query identityTitle, subjects []identityTitle, catalog Catalog, hasFileTitle bool) nameHit {
	best := nameHit{kind: NameNone, titleIdx: -1}
	for _, sub := range subjects {
		h := compareNames(query, sub, catalog)
		h.queryText = query.text
		h.querySource = query.source
		h.subjectText = sub.text
		h.subjectRole = sub.role
		if betterNameHit(h, best, hasFileTitle) {
			best = h
		}
	}
	return best
}

func compareNames(q, s identityTitle, catalog Catalog) nameHit {
	mk := func(score float64, kind NameMatchKind, support string) nameHit {
		return nameHit{score: score, kind: kind, support: support}
	}

	// 1. 严格全名
	if q.forms.Strict != "" && q.forms.Strict == s.forms.Strict {
		return mk(nameScoreStrictEqual, NameFullStrict, "严格全名相等")
	}
	// 2. 常规全名
	if q.forms.Normal != "" && q.forms.Normal == s.forms.Normal {
		return mk(nameScoreNormalEqual, NameFullNormal, "常规全名相等")
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
		// 若 base 与原全名相同，仍算 full；否则 base
		if qBaseForms.Strict == q.forms.Strict && sBaseForms.Strict == s.forms.Strict {
			return mk(nameScoreStrictEqual, NameFullStrict, "严格全名相等")
		}
		return mk(nameScoreStrictEqual, NameBaseStrict, "严格base相等")
	}
	if qBaseForms.Normal != "" && qBaseForms.Normal == sBaseForms.Normal {
		if qBaseForms.Normal == q.forms.Normal && sBaseForms.Normal == s.forms.Normal {
			return mk(nameScoreNormalEqual, NameFullNormal, "常规全名相等")
		}
		return mk(nameScoreNormalEqual, NameBaseNormal, "常规base相等")
	}

	// 3. catalog 精确系列 base 等价
	if catalogBaseEquiv(qBase, sBase, catalog) || catalogBaseEquiv(q.text, s.text, catalog) {
		return mk(nameScoreCatalogEquiv, NameCatalog, "catalog系列base等价")
	}

	// 4. 副标题（完整主标题段 + 认可边界）
	if detectSubtitleMain(qBase, sBase) || detectSubtitleMain(q.text, s.text) {
		return mk(nameScoreWeakCap, NameSubtitle, "完整主标题副标题命中")
	}

	// 5. Loose
	looseHit := nameHit{kind: NameNone}
	if qBaseForms.Loose != "" && qBaseForms.Loose == sBaseForms.Loose {
		looseHit = mk(nameScoreWeakCap, NameLoose, "Loose命中")
	} else if q.forms.Loose != "" && q.forms.Loose == s.forms.Loose {
		looseHit = mk(nameScoreWeakCap, NameLoose, "Loose全名命中")
	}

	// 6. 模糊 / 普通前缀
	fuzzy := diceWeight*multisetDiceBigram(qBaseForms.Normal, sBaseForms.Normal) +
		lcsWeight*lcsRatio(qBaseForms.Normal, sBaseForms.Normal)
	prefixHit := nameHit{kind: NameNone}
	if isOrdinaryPrefixContain(qBaseForms.Normal, sBaseForms.Normal) {
		capScore := fuzzy
		if capScore > nameScoreWeakCap {
			capScore = nameScoreWeakCap
		}
		if capScore <= 0 {
			capScore = nameScoreWeakCap * 0.5
		}
		prefixHit = mk(capScore, NamePrefix, "普通前缀包含")
	}
	fuzzyHit := nameHit{kind: NameNone}
	if fuzzy > 0 && prefixHit.kind == NameNone {
		fuzzyHit = mk(fuzzy, NameFuzzy, "模糊名称")
	}

	best := nameHit{kind: NameNone}
	for _, h := range []nameHit{prefixHit, fuzzyHit, looseHit} {
		if betterNameHit(h, best, true) {
			best = h
		}
	}
	return best
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

	// 季：S01 对未知季号的 Missing 由策略层按规则许可生成，此处 defer
	if q.Ordinal.Season > 0 {
		if subOrd.Season > 0 {
			if q.Ordinal.Season != subOrd.Season {
				c.Blockers = appendUniqueString(c.Blockers,
					fmt.Sprintf("季号冲突：查询%d vs 候选%d", q.Ordinal.Season, subOrd.Season))
			} else {
				c.Support = appendUniqueString(c.Support, "季号相容")
			}
		} else if q.Ordinal.Season != 1 {
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
			c.Missing = appendUniqueString(c.Missing, "候选无法解释Final")
		} else {
			c.Support = appendUniqueString(c.Support, "Final相容")
		}
	} else if subOrd.Final && (q.Ordinal.Season > 0 || q.Media == KindSeries) {
		c.Blockers = appendUniqueString(c.Blockers, "候选为Final与查询结构不符")
	}

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
	pu := uniquePositive(parts)
	if len(pu) == 1 {
		ord.Part = pu[0]
	}
	if finals > 0 && finals == 1+len(sub.Aliases) || (finals > 0 && len(su) <= 1) {
		ord.Final = finals > 0
	}
	return ord
}

func mediaCompatible(a, b MediaKind) bool {
	if a == b {
		return true
	}
	if a == KindSeries && b == KindSeries {
		return true
	}
	return false
}

func applyDiscriminatorConflicts(c *Candidate, idents []identityTitle, subNames []identityTitle) {
	qDisc := map[string]struct{}{}
	var qDiscList []string
	for _, id := range idents {
		for _, d := range id.facts.Discriminators {
			nd := normalizeDisc(d)
			if _, ok := qDisc[nd]; !ok {
				qDiscList = append(qDiscList, d)
			}
			qDisc[nd] = struct{}{}
		}
	}
	sDisc := map[string]struct{}{}
	var sDiscList []string
	for _, sn := range subNames {
		for _, d := range sn.facts.Discriminators {
			nd := normalizeDisc(d)
			if _, ok := sDisc[nd]; !ok {
				sDiscList = append(sDiscList, d)
			}
			sDisc[nd] = struct{}{}
		}
	}
	if len(qDisc) == 0 || len(sDisc) == 0 {
		return
	}
	// 数字称号冲突（No.8 vs No.9 / U-17 vs U-18）
	for _, conf := range ConflictingDigitIdentities(qDiscList, sDiscList) {
		c.Blockers = appendUniqueString(c.Blockers, formatDigitConflictBlocker(conf))
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
	compact := strings.ReplaceAll(TitleKey(text), " ", "")
	return utf8.RuneCountInString(compact) <= shortLatinMaxRunes
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
