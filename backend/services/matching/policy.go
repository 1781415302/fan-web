package matching

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const variantPreferencePenalty = 0.13

// Part归属缺证文案；属身份类 Missing，不得进入 OnlyOrdinalStructureMissing。
const missingPartAffiliation = "Part归属需完整章节证明"

// evaluateCandidatePolicy 给单候选生成本阶段的缺证/支持与必要章节标志，不执行最终接受。
func evaluateCandidatePolicy(q Query, sub Subject, name NameEvidence, ch ChapterDecision) CandidatePolicy {
	var pol CandidatePolicy

	evalFirstSeasonCompat(q, sub, name, ch, &pol)
	evalSubtitleVerified(q, sub, name, ch, &pol)
	evalPartByChapters(q, sub, name, ch, &pol)

	pol.AppliedRules = orderedUniqueRules(pol.AppliedRules)
	return pol
}

// applyVariantPreference 在同组有效完整主标题参考项存在时，
// 降低未经查询指定的派生条目得分；不改 NameScore。
func applyVariantPreference(q Query, ranked []Candidate) []Candidate {
	if len(ranked) == 0 {
		return ranked
	}
	refIdx := findVariantReferenceIndexes(q, ranked)
	if len(refIdx) == 0 {
		return ranked
	}
	out := make([]Candidate, len(ranked))
	copy(out, ranked)
	refIDs := make(map[int]bool, len(refIdx))
	for _, i := range refIdx {
		refIDs[out[i].Subject.ID] = true
		out[i].AppliedRules = orderedUniqueRules(append(out[i].AppliedRules, RuleVariantPreference))
	}
	for i := range out {
		if refIDs[out[i].Subject.ID] {
			continue
		}
		if !variantSharesQueryIdentity(out[i]) {
			continue
		}
		if !variantNeedsPenalty(q, out[i].Subject) {
			continue
		}
		// 先算原 Score 并 clamp，再扣减并 clamp；不叠加。
		out[i].Score = clamp01(out[i].Score - variantPreferencePenalty)
	}
	return out
}

func evalFirstSeasonCompat(q Query, sub Subject, name NameEvidence, ch ChapterDecision, pol *CandidatePolicy) {
	if q.Ordinal.Season != 1 {
		return
	}
	subOrd := subjectOrdinal(sub)
	if subOrd.Season > 0 {
		// 已有显式季号：由结构层处理，不在此推断
		return
	}
	if !sub.Detailed || sub.Media != KindSeries {
		pol.Missing = appendUniqueString(pol.Missing, "候选无法解释查询季号")
		return
	}
	if !nameKindStrongIdentity(name.Kind) || !name.DirectSupport {
		pol.Missing = appendUniqueString(pol.Missing, "候选无法解释查询季号")
		return
	}
	if subjectHasDisallowedSequelMarkers(sub) {
		pol.Missing = appendUniqueString(pol.Missing, "候选无法解释查询季号")
		return
	}
	if len(q.Conflicts) > 0 {
		pol.Missing = appendUniqueString(pol.Missing, "候选无法解释查询季号")
		return
	}
	// 章节/绑定
	switch {
	case hasFirstSeasonChapterEvidence(ch):
		pol.AppliedRules = append(pol.AppliedRules, RuleFirstSeasonCompat)
		pol.Support = appendUniqueString(pol.Support, "首季兼容可解释查询季号")
		pol.NeedsChapterProof = false
	case ch.State == ChapterUnchecked:
		pol.Missing = appendUniqueString(pol.Missing, "候选无法解释查询季号")
		pol.NeedsChapterProof = true
	default:
		pol.Missing = appendUniqueString(pol.Missing, "候选无法解释查询季号")
		if ch.State == ChapterConflict {
			// 冲突时不宣称尚可证明
			pol.NeedsChapterProof = false
		} else {
			pol.NeedsChapterProof = true
		}
	}
}

func evalSubtitleVerified(q Query, sub Subject, name NameEvidence, ch ChapterDecision, pol *CandidatePolicy) {
	if name.Kind != NameSubtitle {
		return
	}
	if q.Ordinal.Season <= 0 {
		return
	}
	subOrd := subjectOrdinal(sub)
	if subOrd.Season != q.Ordinal.Season || subOrd.Season <= 0 {
		return
	}
	if !sub.Detailed {
		return
	}
	qMedia, sMedia := q.Media, sub.Media
	if qMedia == "" {
		qMedia = KindUnknown
	}
	if sMedia == "" {
		sMedia = KindUnknown
	}
	if qMedia != KindUnknown && sMedia != KindUnknown && !mediaCompatible(qMedia, sMedia) {
		return
	}
	if subjectHasUnexplainedPartOrFinal(q, subOrd) {
		return
	}
	if !coverageFullyMatched(ch) {
		if ch.State == ChapterUnchecked || len(ch.Coverage.Required) > 0 {
			pol.NeedsChapterProof = true
		}
		return
	}
	pol.AppliedRules = append(pol.AppliedRules, RuleSubtitleVerified)
	pol.Support = appendUniqueString(pol.Support, "副标题完整主标题已验证")
	pol.NeedsChapterProof = false
}

func evalPartByChapters(q Query, sub Subject, name NameEvidence, ch ChapterDecision, pol *CandidatePolicy) {
	if q.Ordinal.Part > 0 {
		// 查询显式 Part：不靠编号猜；结构层 Missing 保留
		return
	}
	subOrd := subjectOrdinal(sub)
	if subOrd.Part <= 0 {
		return
	}
	if partIndicatedBySharedMainTitle(q, sub, subOrd.Part) {
		return
	}
	if coverageFullyMatched(ch) {
		pol.AppliedRules = append(pol.AppliedRules, RulePartByChapters)
		pol.Support = appendUniqueString(pol.Support, "Part归属由完整章节覆盖证明")
		pol.NeedsChapterProof = false
		return
	}
	pol.Missing = appendUniqueString(pol.Missing, missingPartAffiliation)
	if ch.State == ChapterUnchecked || (len(ch.Coverage.Required) > 0 && len(ch.Coverage.Missing) > 0) {
		pol.NeedsChapterProof = true
	}
	_ = name // 保留签名对称；归属不依赖 Kind
}

func nameKindStrongIdentity(k NameMatchKind) bool {
	switch k {
	case NameFullStrict, NameFullNormal, NameBaseStrict, NameBaseNormal, NameCatalog:
		return true
	default:
		return false
	}
}

func subjectHasDisallowedSequelMarkers(sub Subject) bool {
	var seasons, parts []int
	var hasFinal bool
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
			hasFinal = true
		}
	}
	add(sub.Name)
	add(sub.NameCn)
	for _, a := range sub.Aliases {
		add(a)
	}
	for _, s := range seasons {
		if s > 1 {
			return true
		}
	}
	for _, p := range parts {
		if p > 1 {
			return true
		}
	}
	if hasFinal {
		return true
	}
	// 互相矛盾的显式季号（如同时出现 1 与 2）
	su := uniquePositive(seasons)
	if len(su) > 1 {
		return true
	}
	pu := uniquePositive(parts)
	if len(pu) > 1 {
		return true
	}
	return false
}

func subjectHasUnexplainedPartOrFinal(q Query, subOrd Ordinal) bool {
	if subOrd.Part > 1 && q.Ordinal.Part == 0 {
		return true
	}
	if subOrd.Final && !q.Ordinal.Final {
		return true
	}
	return false
}

func hasFirstSeasonChapterEvidence(ch ChapterDecision) bool {
	if ch.State == ChapterCountFit {
		return true
	}
	if ch.State == ChapterConflict {
		return false
	}
	for _, b := range ch.Bindings {
		if b.StoredNumber > 0 {
			return true
		}
	}
	return false
}

func coverageFullyMatched(ch ChapterDecision) bool {
	if len(ch.Coverage.Required) == 0 {
		return false
	}
	if len(ch.Coverage.Missing) != 0 {
		return false
	}
	if len(ch.Coverage.Matched) != len(ch.Coverage.Required) {
		return false
	}
	for i := range ch.Coverage.Required {
		if ch.Coverage.Required[i] != ch.Coverage.Matched[i] {
			return false
		}
	}
	return true
}

func partIndicatedBySharedMainTitle(q Query, sub Subject, part int) bool {
	if part <= 0 {
		return false
	}
	subNames := collectSubjectNameTexts(sub)
	for _, qt := range q.Titles {
		if qt.Source == SourceCatalog {
			continue
		}
		qf := AnalyzeTitle(qt.Text)
		if qf.Ordinal.Part != part {
			continue
		}
		for _, sn := range subNames {
			sf := AnalyzeTitle(sn)
			if sf.Ordinal.Part != part {
				continue
			}
			if fullOrBaseEqual(qt.Text, sn) {
				return true
			}
		}
	}
	return false
}

func collectSubjectNameTexts(sub Subject) []string {
	out := make([]string, 0, 2+len(sub.Aliases))
	if strings.TrimSpace(sub.Name) != "" {
		out = append(out, sub.Name)
	}
	if strings.TrimSpace(sub.NameCn) != "" {
		out = append(out, sub.NameCn)
	}
	out = append(out, sub.Aliases...)
	return out
}

func fullOrBaseEqual(a, b string) bool {
	fa, fb := Normalize(a), Normalize(b)
	if fa.Strict != "" && fa.Strict == fb.Strict {
		return true
	}
	if fa.Normal != "" && fa.Normal == fb.Normal {
		return true
	}
	ab := strings.TrimSpace(AnalyzeTitle(a).Base)
	bb := strings.TrimSpace(AnalyzeTitle(b).Base)
	if ab == "" {
		ab = strings.TrimSpace(a)
	}
	if bb == "" {
		bb = strings.TrimSpace(b)
	}
	ba, bb2 := Normalize(ab), Normalize(bb)
	if ba.Strict != "" && ba.Strict == bb2.Strict {
		return true
	}
	if ba.Normal != "" && ba.Normal == bb2.Normal {
		return true
	}
	return false
}

func findVariantReferenceIndexes(q Query, ranked []Candidate) []int {
	var refs []int
	for i, c := range ranked {
		if !isVariantReference(q, c) {
			continue
		}
		refs = append(refs, i)
	}
	return refs
}

func isVariantReference(q Query, c Candidate) bool {
	if !c.Subject.Detailed {
		return false
	}
	if c.Subject.Media != KindSeries {
		return false
	}
	if len(c.Blockers) > 0 || len(c.Missing) > 0 {
		return false
	}
	if c.NameScore < nameScoreAcceptMin || c.Score < scoreAcceptMin {
		return false
	}
	switch c.NameEvidence.Kind {
	case NameFullStrict, NameFullNormal:
		// ok：完整主标题严格/常规；不允许仅 base/catalog
	default:
		return false
	}
	if c.NameEvidence.QuerySource == SourceCatalog {
		return false
	}
	if q.HasFileTitle {
		if c.NameEvidence.QuerySource != SourceFile {
			return false
		}
	} else if c.NameEvidence.QuerySource != SourceDirectory && c.NameEvidence.QuerySource != SourceFile {
		return false
	}
	if subjectHasDisallowedSequelMarkers(c.Subject) {
		return false
	}
	subOrd := subjectOrdinal(c.Subject)
	if subOrd.Season > 1 || subOrd.Part > 1 || subOrd.Final {
		return false
	}
	return true
}

func variantSharesQueryIdentity(c Candidate) bool {
	switch c.NameEvidence.Kind {
	case NameFullStrict, NameFullNormal, NameBaseStrict, NameBaseNormal, NameCatalog:
		return true
	default:
		return false
	}
}

func variantNeedsPenalty(q Query, sub Subject) bool {
	subOrd := subjectOrdinal(sub)
	need := false
	if subOrd.Season > 1 && q.Ordinal.Season == 0 {
		need = true
	}
	if subOrd.Part > 1 && q.Ordinal.Part == 0 {
		need = true
	}
	if subOrd.Final && !q.Ordinal.Final {
		need = true
	}
	if sub.Media == KindMovie && q.Media != KindMovie {
		need = true
	}
	if sub.Media == KindRecap && q.Media != KindRecap {
		need = true
	}
	// 别名中的续季/Part 也视为派生限定
	if !need && q.Ordinal.Season == 0 && subjectHasDisallowedSequelMarkers(sub) {
		need = true
	}
	return need
}

func orderedUniqueRules(rules []MatchRule) []MatchRule {
	if len(rules) == 0 {
		return nil
	}
	order := []MatchRule{
		RuleFirstSeasonCompat,
		RuleSubtitleVerified,
		RulePartByChapters,
		RuleVariantPreference,
	}
	seen := map[MatchRule]bool{}
	for _, r := range rules {
		seen[r] = true
	}
	out := make([]MatchRule, 0, len(seen))
	for _, r := range order {
		if seen[r] {
			out = append(out, r)
		}
	}
	return out
}

// mergePolicyIntoCandidate 将策略结果并入候选；由规则许可控制 Missing，不删除中文诊断串。
// fragmentConflict 为真时，首季/副标题晋升条件 4 不成立。
func mergePolicyIntoCandidate(c *Candidate, pol CandidatePolicy, q Query, fragmentConflict bool, ch ChapterDecision) {
	subOrd := subjectOrdinal(c.Subject)

	// 复制策略 Blockers/Support
	for _, b := range pol.Blockers {
		c.Blockers = appendUniqueString(c.Blockers, b)
	}
	for _, s := range pol.Support {
		c.Support = appendUniqueString(c.Support, s)
	}

	rules := append([]MatchRule(nil), pol.AppliedRules...)
	needsProof := pol.NeedsChapterProof

	if fragmentConflict || len(c.Blockers) > 0 || len(q.Conflicts) > 0 {
		// 撤销依赖“无冲突”的晋升规则
		filtered := rules[:0]
		for _, r := range rules {
			if r == RuleFirstSeasonCompat || r == RuleSubtitleVerified {
				continue
			}
			filtered = append(filtered, r)
		}
		rules = filtered
		if q.Ordinal.Season == 1 && subOrd.Season == 0 && !hasRule(rules, RuleFirstSeasonCompat) {
			c.Missing = appendUniqueString(c.Missing, "候选无法解释查询季号")
		}
		needsProof = false
	}

	hasFirst := hasRule(rules, RuleFirstSeasonCompat)
	hasSub := hasRule(rules, RuleSubtitleVerified)

	// 首季：未获许可时写入季号 Missing（结构层对 S01 已 defer）
	if q.Ordinal.Season == 1 && subOrd.Season == 0 && !hasFirst {
		c.Missing = appendUniqueString(c.Missing, "候选无法解释查询季号")
	}

	// 弱名称 Missing：副标题仅在已验证时跳过
	switch c.NameEvidence.Kind {
	case NameSubtitle:
		if !hasSub {
			c.Missing = appendUniqueString(c.Missing, "弱形态身份支持不能自动接受")
		}
	case NamePrefix, NameLoose:
		c.Missing = appendUniqueString(c.Missing, "弱形态身份支持不能自动接受")
	case NameFuzzy:
		if isShortTitle(c.NameEvidence.QueryText) || isShortTitle(nameEvidenceBase(c.NameEvidence)) {
			c.Missing = appendUniqueString(c.Missing, "短标题仅允许精确或catalog等价")
		}
	}

	for _, m := range pol.Missing {
		if m == "候选无法解释查询季号" && hasFirst {
			continue
		}
		if m == "弱形态身份支持不能自动接受" && hasSub {
			continue
		}
		c.Missing = appendUniqueString(c.Missing, m)
	}

	c.AppliedRules = orderedUniqueRules(append(c.AppliedRules, rules...))
	c.NeedsChapterProof = c.NeedsChapterProof || needsProof
	_ = ch
}

func hasRule(rules []MatchRule, want MatchRule) bool {
	for _, r := range rules {
		if r == want {
			return true
		}
	}
	return false
}

func nameEvidenceBase(n NameEvidence) string {
	if strings.TrimSpace(n.QueryText) == "" {
		return ""
	}
	b := strings.TrimSpace(AnalyzeTitle(n.QueryText).Base)
	if b != "" {
		return b
	}
	return n.QueryText
}

// nameKindRank 同分时优先级：越小越优先。
func nameKindRank(k NameMatchKind) int {
	switch k {
	case NameFullStrict:
		return 0
	case NameFullNormal:
		return 1
	case NameBaseStrict:
		return 2
	case NameBaseNormal:
		return 3
	case NameCatalog:
		return 4
	case NameSubtitle:
		return 5
	case NamePrefix:
		return 6
	case NameFuzzy:
		return 7
	case NameLoose:
		return 8
	default:
		return 9
	}
}

func sourcePreferRank(s Source, hasFileTitle bool) int {
	switch s {
	case SourceFile:
		return 0
	case SourceDirectory:
		if !hasFileTitle {
			return 0
		}
		return 2
	case SourceCatalog:
		return 3
	default:
		return 4
	}
}

func subjectRoleRank(r SubjectNameRole) int {
	if r == SubjectPrimary {
		return 0
	}
	return 1
}

// detectSubtitleMain 若 shorter 等于 longer 在认可副标题边界前的完整主标题段，返回 true。
func detectSubtitleMain(shorter, longer string) bool {
	shorter = strings.TrimSpace(shorter)
	longer = strings.TrimSpace(longer)
	if shorter == "" || longer == "" || shorter == longer {
		return false
	}
	// 保证 shorter 确实更短（按 rune）
	if utf8.RuneCountInString(shorter) >= utf8.RuneCountInString(longer) {
		shorter, longer = longer, shorter
		if utf8.RuneCountInString(shorter) >= utf8.RuneCountInString(longer) {
			return false
		}
	}
	sf := Normalize(shorter)
	tryMain := func(main string) bool {
		main = strings.TrimSpace(main)
		if main == "" {
			return false
		}
		mf := Normalize(main)
		if sf.Strict != "" && sf.Strict == mf.Strict {
			return true
		}
		if sf.Normal != "" && sf.Normal == mf.Normal {
			return true
		}
		return false
	}
	// 冒号
	for _, sep := range []string{"：", ":"} {
		if i := indexSep(longer, sep); i > 0 {
			if tryMain(longer[:i]) {
				return true
			}
		}
	}
	// 两侧有空格的破折号
	for _, sep := range []string{" — ", " – ", " - "} {
		if i := strings.Index(longer, sep); i > 0 {
			if tryMain(longer[:i]) {
				return true
			}
		}
	}
	// 成对 ～…～ / ~…~
	if main, ok := pairedWaveMain(longer, '～'); ok && tryMain(main) {
		return true
	}
	if main, ok := pairedWaveMain(longer, '~'); ok && tryMain(main) {
		return true
	}
	return false
}

func indexSep(s, sep string) int {
	return strings.Index(s, sep)
}

func pairedWaveMain(s string, wave rune) (string, bool) {
	rs := []rune(s)
	var idxs []int
	for i, r := range rs {
		if r == wave {
			idxs = append(idxs, i)
		}
	}
	if len(idxs) < 2 {
		return "", false
	}
	// 主标题为第一对波纹前的部分
	main := strings.TrimSpace(string(rs[:idxs[0]]))
	if main == "" {
		return "", false
	}
	return main, true
}

// isOrdinaryPrefixContain 普通前缀（无副标题边界）。
func isOrdinaryPrefixContain(a, b string) bool {
	if a == "" || b == "" || a == b {
		return false
	}
	shorter, longer := a, b
	if len(a) > len(b) {
		shorter, longer = b, a
	}
	if detectSubtitleMain(shorter, longer) {
		return false
	}
	if strings.HasPrefix(longer, shorter) {
		return true
	}
	// 宽松包含且较短达到较长一半以上，视为弱前缀/包含
	if strings.Contains(longer, shorter) && utf8.RuneCountInString(shorter)*2 >= utf8.RuneCountInString(longer) {
		return true
	}
	return false
}

func formatDigitConflictBlocker(conf string) string {
	return fmt.Sprintf("区分符冲突：%s", conf)
}
