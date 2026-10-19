package services

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"fan-web/services/matching"
)

// MatchingSession 本次操作独享缓存、预算、降级状态；唯一在线 Resolve 入口。
type MatchingSession struct {
	runtime  *MatchingRuntime
	ctx      context.Context
	provider matchingProvider

	// pool 为 Session 内共享的条目缓存（搜索/详情可复用）；保留已校验 Detailed。
	pool map[int]matching.Subject
	// active 为当前分组候选 ID 集合：仅本组实际召回 + 本次 bound/target。
	// 其他查询缓存过的条目不得自动进入本组评分。
	active map[int]struct{}
}

// BoundMatch 目录已绑定条目的快通道证据；存在 bound 绝不能绕过结构检查。
type BoundMatch struct {
	Subject         matching.Subject
	UniqueDirectory bool // 本目录恰好绑定该一部；由调用方查询确定
}

// MatchResult Resolve / ResolveForTarget 的统一输出。
type MatchResult struct {
	Decision          matching.Decision
	Subject           *matching.Subject // Accepted 非 nil 且 ID==WinnerID；新条目必须 Detailed=true
	FromExistingBound bool              // 安全快通道复用 DB 条目可 Detailed=false
	ManuallyConfirmed bool              // 仅 ResolveForTarget 可为 true
	Chapters          matching.ChapterDecision
	Queries           []matching.QueryTerm
}

const (
	sessionChapterProbeBudget = 6
	sessionAcceptDistance     = 0.04
	sessionNameScoreProbeMin  = 0.80 // 与 matching.nameScoreProbeMin 对齐
	sessionNameScoreAcceptMin = 0.88
	sessionScoreAcceptMin     = 0.92
)

func emptyMatchResult() MatchResult {
	return MatchResult{
		Decision: matching.Decision{Candidates: []matching.Candidate{}},
		Queries:  []matching.QueryTerm{},
		Chapters: matching.ChapterDecision{
			Bindings: []matching.EpisodeBinding{},
			Rejected: map[string]string{},
		},
	}
}

// preferEligibleOrder 稳定地把可自动接受的候选移到前面，保留相对顺序；不改分数。
// 优先：无 Blockers 且无 Missing；其次仅 Missing；最后带 Blockers。
// 避免同分时 ID 升序把「无法解释季号」的候选排在可接受候选之前。
func preferEligibleOrder(ranked []matching.Candidate) []matching.Candidate {
	if len(ranked) < 2 {
		return ranked
	}
	var ok, soft, bad []matching.Candidate
	for _, c := range ranked {
		switch {
		case len(c.Blockers) > 0:
			bad = append(bad, c)
		case len(c.Missing) > 0:
			soft = append(soft, c)
		default:
			ok = append(ok, c)
		}
	}
	if len(ok) == 0 && len(soft) == 0 {
		return ranked
	}
	if len(ok) == len(ranked) {
		return ranked
	}
	out := make([]matching.Candidate, 0, len(ranked))
	out = append(out, ok...)
	out = append(out, soft...)
	out = append(out, bad...)
	return out
}

func normalizeGroupMedia(g matching.Group) matching.Group {
	if g.Query.Media == "" {
		g.Query.Media = matching.KindUnknown
	}
	for i := range g.Files {
		if g.Files[i].Media == "" {
			g.Files[i].Media = matching.KindUnknown
		}
	}
	return g
}

func (s *MatchingSession) ensurePool() {
	if s.pool == nil {
		s.pool = make(map[int]matching.Subject)
	}
}

func (s *MatchingSession) ensureActive() {
	if s.active == nil {
		s.active = make(map[int]struct{})
	}
}

func (s *MatchingSession) resetActive() {
	s.active = make(map[int]struct{})
}

// mergeSubjectActive 写入 Session 缓存，并纳入当前分组候选集合。
func (s *MatchingSession) mergeSubjectActive(sub matching.Subject) {
	s.mergeSubject(sub)
	if sub.ID <= 0 {
		return
	}
	s.ensureActive()
	s.active[sub.ID] = struct{}{}
}

func (s *MatchingSession) catalog() matching.Catalog {
	if s == nil || s.runtime == nil {
		return matching.Catalog{}
	}
	return s.runtime.Catalog()
}

func (s *MatchingSession) mergeSubject(sub matching.Subject) {
	if sub.ID <= 0 {
		return
	}
	s.ensurePool()
	prev, ok := s.pool[sub.ID]
	if !ok {
		s.pool[sub.ID] = cloneSubject(sub)
		return
	}
	if prev.Detailed && !sub.Detailed {
		// 保留已校验详情，合并别名
		s.pool[sub.ID] = mergeSubjectPreferDetailed(prev, sub)
		return
	}
	if sub.Detailed {
		s.pool[sub.ID] = mergeSubjectPreferDetailed(sub, prev)
		return
	}
	s.pool[sub.ID] = mergeSubjectPreferDetailed(prev, sub)
}

func mergeSubjectPreferDetailed(primary, secondary matching.Subject) matching.Subject {
	out := primary
	if !out.Detailed && secondary.Detailed {
		out = secondary
		secondary = primary
	}
	out.Aliases = mergeAliasesStable(out.Aliases, secondary.Aliases)
	if strings.TrimSpace(out.Name) == "" {
		out.Name = secondary.Name
	}
	if strings.TrimSpace(out.NameCn) == "" {
		out.NameCn = secondary.NameCn
	}
	if out.Year == 0 {
		out.Year = secondary.Year
	}
	if out.Media == matching.KindUnknown {
		out.Media = secondary.Media
	}
	if out.EpisodeCount == 0 {
		out.EpisodeCount = secondary.EpisodeCount
	}
	if strings.TrimSpace(out.Summary) == "" {
		out.Summary = secondary.Summary
	}
	if strings.TrimSpace(out.Cover) == "" {
		out.Cover = secondary.Cover
	}
	if strings.TrimSpace(out.Platform) == "" {
		out.Platform = secondary.Platform
	}
	return out
}

func mergeAliasesStable(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	add := func(xs []string) {
		for _, x := range xs {
			x = strings.TrimSpace(x)
			if x == "" {
				continue
			}
			key := matching.TitleKey(x)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, x)
		}
	}
	add(a)
	add(b)
	return out
}

func (s *MatchingSession) poolSlice() []matching.Subject {
	s.ensurePool()
	s.ensureActive()
	ids := make([]int, 0, len(s.active))
	for id := range s.active {
		if _, ok := s.pool[id]; !ok {
			continue
		}
		ids = append(ids, id)
	}
	sort.Ints(ids)
	out := make([]matching.Subject, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneSubject(s.pool[id]))
	}
	return out
}

// Resolve 对分组做自动匹配：查询调度、缓存、详情确认、章节探测、快通道。
func (s *MatchingSession) Resolve(group matching.Group, bound *BoundMatch) (MatchResult, error) {
	if s == nil {
		return emptyMatchResult(), fmt.Errorf("MatchingSession.Resolve: session 为空")
	}
	if s.runtime == nil {
		return emptyMatchResult(), fmt.Errorf("MatchingSession.Resolve: runtime 为空")
	}
	if s.provider == nil {
		return emptyMatchResult(), fmt.Errorf("MatchingSession.Resolve: provider 为空")
	}
	if s.ctx == nil {
		s.ctx = context.Background()
	}
	s.ensurePool()
	return s.resolve(group, bound, 0, false)
}

// ResolveForTarget 先做相同自动判断，再以明确目标确认。
func (s *MatchingSession) ResolveForTarget(group matching.Group, target matching.Subject) (MatchResult, error) {
	if s == nil {
		return emptyMatchResult(), fmt.Errorf("MatchingSession.ResolveForTarget: session 为空")
	}
	if s.runtime == nil {
		return emptyMatchResult(), fmt.Errorf("MatchingSession.ResolveForTarget: runtime 为空")
	}
	if s.provider == nil {
		return emptyMatchResult(), fmt.Errorf("MatchingSession.ResolveForTarget: provider 为空")
	}
	if target.ID <= 0 {
		return emptyMatchResult(), fmt.Errorf("MatchingSession.ResolveForTarget: 无效目标 ID")
	}
	if s.ctx == nil {
		s.ctx = context.Background()
	}
	s.ensurePool()
	return s.resolve(group, nil, target.ID, true)
}

func (s *MatchingSession) resolve(group matching.Group, bound *BoundMatch, targetID int, forTarget bool) (MatchResult, error) {
	out := emptyMatchResult()
	group = normalizeGroupMedia(group)
	q := group.Query
	catalog := s.catalog()
	s.resetActive()

	// —— 快通道：bound 仅为候选证据，绝不绕过结构检查 ——
	if bound != nil && bound.UniqueDirectory && bound.Subject.ID > 0 && !forTarget {
		fast, ok, err := s.tryFastPath(group, *bound)
		if err != nil {
			return emptyMatchResult(), err
		}
		if ok {
			return fast, nil
		}
		// 快通道不适用：bound 仅作候选进入本组，不豁免结构检查；继续召回
		s.mergeSubjectActive(bound.Subject)
	}
	if forTarget && targetID > 0 {
		// 目标在详情补全前先占位进入本组候选
		if sub, ok := s.pool[targetID]; ok {
			s.mergeSubjectActive(sub)
		} else {
			s.mergeSubjectActive(matching.Subject{ID: targetID})
		}
	}

	terms := matching.BuildQueries(q, catalog)
	out.Queries = append([]matching.QueryTerm(nil), terms...)

	retrievalComplete, err := s.runQueries(q, terms, catalog)
	if err != nil {
		return emptyMatchResult(), err
	}

	if forTarget && targetID > 0 {
		if err := s.ensureDetailed(targetID); err != nil {
			// 目标详情失败：仍继续自动路径；ConfirmTarget 可能因目标不在池而 Review
			// 但基础设施错误应上抛
			return emptyMatchResult(), err
		}
	}

	chapters := map[int]matching.ChapterDecision{}
	probed := map[int]bool{}
	var winnerChapters matching.ChapterDecision

	// 收敛：初排 → 章节探测/详情补全 → 再排 → Decide；最多 sessionChapterProbeBudget 次探测
	for round := 0; round < sessionChapterProbeBudget+3; round++ {
		subjects := s.poolSlice()
		ranked := matching.Rank(q, subjects, catalog, chapters)

		needProbe := s.selectChapterProbes(q, ranked, probed, chapters)
		if len(needProbe) > 0 {
			for _, id := range needProbe {
				if len(probed) >= sessionChapterProbeBudget {
					break
				}
				if probed[id] {
					continue
				}
				dec, err := s.probeChapters(group, id)
				if err != nil {
					return emptyMatchResult(), err
				}
				chapters[id] = dec
				probed[id] = true
			}
			continue // 重新 Rank
		}

		// 接受前确保胜者详情（非 FromExistingBound）
		ranked = matching.Rank(q, s.poolSlice(), catalog, chapters)
		// 同分时 Rank 按 ID 升序，可能把「季冲突」候选排在可接受候选前；
		// Decide 以 ranked[0] 为 best，故在不改 Score/Blockers 的前提下稳定前移无 Blockers 者。
		ranked = preferEligibleOrder(ranked)
		var decision matching.Decision
		if forTarget {
			auto := matching.Decide(q, ranked, retrievalComplete)
			if auto.Code == matching.Accepted && auto.WinnerID != targetID {
				decision = auto
				decision.Reason = "自动决定已接受其他作品，不覆盖为目标"
			} else if auto.Code == matching.Accepted && auto.WinnerID == targetID {
				decision = auto
			} else {
				decision = matching.ConfirmTarget(q, ranked, targetID)
			}
		} else {
			decision = matching.Decide(q, ranked, retrievalComplete)
		}

		if decision.Code == matching.Accepted && decision.WinnerID > 0 {
			sub, ok := s.pool[decision.WinnerID]
			if !ok {
				return emptyMatchResult(), fmt.Errorf("胜者 %d 不在候选池: %w", decision.WinnerID, ErrMatchUpstream)
			}
			if !sub.Detailed {
				if err := s.ensureDetailed(decision.WinnerID); err != nil {
					return emptyMatchResult(), err
				}
				// 详情可能改变结构 → 再轮
				continue
			}
			// 胜者章节：若未探测且有编号，做一次解释（详情话数 / 已缓存章节）
			if !probed[decision.WinnerID] {
				dec, err := s.probeChapters(group, decision.WinnerID)
				if err != nil {
					return emptyMatchResult(), err
				}
				chapters[decision.WinnerID] = dec
				probed[decision.WinnerID] = true
				// 若章节引入 Conflict，必须重新判断，不可先接受再探测
				if dec.State == matching.ChapterConflict {
					continue
				}
				winnerChapters = dec
			} else {
				winnerChapters = chapters[decision.WinnerID]
			}

			out.Decision = decision
			cloned := cloneSubject(s.pool[decision.WinnerID])
			out.Subject = &cloned
			out.Chapters = normalizeChapterDecision(winnerChapters)
			out.ManuallyConfirmed = forTarget
			return out, nil
		}

		out.Decision = decision
		if decision.Code == matching.Accepted {
			// 不应到达
			break
		}
		// Review / NoCandidates：附带最高候选的章节态（若有）
		if len(decision.Candidates) > 0 {
			id := decision.Candidates[0].Subject.ID
			if ch, ok := chapters[id]; ok {
				out.Chapters = normalizeChapterDecision(ch)
			}
		}
		out.ManuallyConfirmed = false
		return out, nil
	}

	out.Decision = matching.Decision{
		Code:       matching.Review,
		Candidates: matching.Rank(q, s.poolSlice(), catalog, chapters),
		Reason:     "探测预算内无法确认胜者",
	}
	if len(out.Decision.Candidates) > 3 {
		out.Decision.Candidates = out.Decision.Candidates[:3]
	}
	if out.Decision.Candidates == nil {
		out.Decision.Candidates = []matching.Candidate{}
	}
	return out, nil
}

func normalizeChapterDecision(ch matching.ChapterDecision) matching.ChapterDecision {
	if ch.Bindings == nil {
		ch.Bindings = []matching.EpisodeBinding{}
	}
	if ch.Rejected == nil {
		ch.Rejected = map[string]string{}
	}
	if ch.State == "" {
		ch.State = matching.ChapterUnchecked
	}
	return ch
}

// tryFastPath 尝试唯一目录安全快通道。ok=false 表示不能走快通道（调用方继续搜索）。
func (s *MatchingSession) tryFastPath(group matching.Group, bound BoundMatch) (MatchResult, bool, error) {
	out := emptyMatchResult()
	q := group.Query
	catalog := s.catalog()
	sub := bound.Subject
	s.mergeSubjectActive(sub)

	// 结构检查：用 Rank 对单候选评估 Blockers/Missing（与正式路径同一套规则）
	ranked := matching.Rank(q, []matching.Subject{sub}, catalog, nil)
	if len(ranked) == 0 {
		return out, false, nil
	}
	c := ranked[0]
	if len(c.Blockers) > 0 {
		// 与旧绑定冲突：只能说明不能复用该绑定，不能判定整文件失败；退出快通道继续召回。
		return out, false, nil
	}
	if len(c.Missing) > 0 {
		// 缺证据：不能把空字段填成相容；退出快通道，进入搜索
		return out, false, nil
	}
	if c.NameScore < sessionNameScoreAcceptMin {
		return out, false, nil
	}

	// 离线追加条件：普通 Single、无版本/类型冲突、原始号在可信 EpCount 内、名称严格/常规等价
	offlineOK := fastPathOfflineEligible(group, sub, c)
	if offlineOK {
		dec := matching.Decide(q, ranked, true)
		if dec.Code != matching.Accepted {
			// 分数/分差不足时不硬走离线
			return out, false, nil
		}
		// 章节按 count 解释（不取网络）
		ch := matching.InterpretChapters(group.Files, sub, nil, true)
		if ch.State == matching.ChapterConflict {
			out.Decision = matching.Decision{
				Code:       matching.Review,
				Candidates: ranked,
				Reason:     "绑定快通道章节冲突",
			}
			out.Chapters = normalizeChapterDecision(ch)
			return out, true, nil
		}
		cloned := cloneSubject(sub)
		out.Decision = matching.Decision{
			Code:       matching.Accepted,
			WinnerID:   sub.ID,
			Candidates: ranked,
			Reason:     "唯一目录安全快通道（复用既有绑定）",
		}
		out.Subject = &cloned
		out.FromExistingBound = true
		out.Chapters = normalizeChapterDecision(ch)
		return out, true, nil
	}

	// 有强身份且无冲突，但需要详情/章节：把 bound 留在池中，继续搜索路径
	return out, false, nil
}

func fastPathOfflineEligible(group matching.Group, sub matching.Subject, c matching.Candidate) bool {
	q := group.Query
	if q.Media != matching.KindUnknown && sub.Media != matching.KindUnknown && q.Media != sub.Media {
		if !(q.Media == matching.KindSeries && sub.Media == matching.KindSeries) {
			return false
		}
	}
	if q.Ordinal.Part > 0 || q.Ordinal.Final {
		return false
	}
	if q.Media == matching.KindOVA || q.Media == matching.KindMovie || q.Media == matching.KindPromo {
		return false
	}
	if sub.Media == matching.KindOVA || sub.Media == matching.KindMovie || sub.Media == matching.KindPromo {
		return false
	}
	// 名称须严格/常规级支持
	strongName := false
	for _, s := range c.Support {
		if strings.Contains(s, "严格") || strings.Contains(s, "常规") {
			strongName = true
			break
		}
	}
	if !strongName && c.NameScore < 0.98 {
		return false
	}
	// 全部明确 Single，且在 EpCount 内
	if sub.EpisodeCount <= 0 {
		return false
	}
	if len(group.Files) == 0 {
		return false
	}
	for _, f := range group.Files {
		if f.Episode.Kind != matching.NumberSingle || f.Episode.First <= 0 {
			return false
		}
		if f.Episode.First > sub.EpisodeCount {
			return false
		}
	}
	return true
}

func (s *MatchingSession) runQueries(q matching.Query, terms []matching.QueryTerm, catalog matching.Catalog) (retrievalComplete bool, err error) {
	if len(terms) == 0 {
		return false, nil
	}

	executed := 0
	anyFileOrCanonical := false
	saturatedUnresolvedSeason := false
	seasonResolved := q.Ordinal.Season <= 0

	// 语言标题是否已由 catalog/别名证明等价：以 TitleKey / catalog.Expand 判断
	type langKey struct {
		key string
	}
	neededLangs := fileTitleLangKeys(q)
	coveredLangs := map[string]bool{}

	for _, term := range terms {
		res, err := s.provider.Search(s.ctx, term.Text)
		if err != nil {
			return false, err
		}
		executed++
		for _, sub := range res.Subjects {
			s.mergeSubjectActive(sub)
		}
		if term.Source == matching.SourceFile || term.Source == matching.SourceCatalog {
			anyFileOrCanonical = true
		}
		if term.Source == matching.SourceDirectory && !q.HasFileTitle {
			anyFileOrCanonical = true
		}

		// 标记语言覆盖：若查询词对应某文件标题或其 catalog 扩展
		for _, lk := range neededLangs {
			if titlesProvenEquivalent(term.Text, lk, catalog, s.pool) {
				coveredLangs[lk] = true
			}
		}

		// 饱和且季未解决：需要带季词
		if res.Saturated && q.Ordinal.Season > 0 && !seasonResolved {
			if queryHasSeasonHint(term.Text, q.Ordinal.Season) {
				// 带季词仍饱和 → 召回不完整
				saturatedUnresolvedSeason = true
			}
		}
		if q.Ordinal.Season > 0 {
			// 仅看本组候选能否解释该季（不得用其他组缓存条目）
			for _, sub := range s.poolSlice() {
				ranked := matching.Rank(q, []matching.Subject{sub}, catalog, nil)
				if len(ranked) == 1 && len(ranked[0].Missing) == 0 && len(ranked[0].Blockers) == 0 && ranked[0].NameScore >= sessionNameScoreProbeMin {
					seasonResolved = true
					saturatedUnresolvedSeason = false
					break
				}
			}
		}

		// 找到足够候选且语言已覆盖时可提前结束
		if len(s.active) > 0 && langsAllCovered(neededLangs, coveredLangs) && (q.Ordinal.Season <= 0 || seasonResolved) {
			// 仍须保证至少执行过一条有效文件/canonical 查询
			if anyFileOrCanonical {
				break
			}
		}
	}

	if !anyFileOrCanonical {
		return false, nil
	}
	if !langsAllCovered(neededLangs, coveredLangs) {
		// 预算内未覆盖各语言标题
		if executed >= len(terms) {
			return false, nil
		}
	}
	if saturatedUnresolvedSeason && !seasonResolved {
		return false, nil
	}
	return true, nil
}

func fileTitleLangKeys(q matching.Query) []string {
	seen := map[string]struct{}{}
	var keys []string
	for _, t := range q.Titles {
		if t.Source == matching.SourceDirectory {
			continue
		}
		base := strings.TrimSpace(matching.AnalyzeTitle(t.Text).Base)
		if base == "" {
			base = strings.TrimSpace(t.Text)
		}
		k := matching.TitleKey(base)
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	return keys
}

func titlesProvenEquivalent(term, langKey string, catalog matching.Catalog, pool map[int]matching.Subject) bool {
	termKey := matching.TitleKey(strings.TrimSpace(matching.AnalyzeTitle(term).Base))
	if termKey == "" {
		termKey = matching.TitleKey(term)
	}
	if termKey == langKey {
		return true
	}
	// catalog 扩展
	for _, et := range catalog.Expand(term) {
		if matching.TitleKey(matching.AnalyzeTitle(et.Text).Base) == langKey || matching.TitleKey(et.Text) == langKey {
			return true
		}
	}
	// 候选官方别名证明等价
	for _, sub := range pool {
		names := []string{sub.Name, sub.NameCn}
		names = append(names, sub.Aliases...)
		hitTerm, hitLang := false, false
		for _, n := range names {
			nk := matching.TitleKey(matching.AnalyzeTitle(n).Base)
			if nk == "" {
				nk = matching.TitleKey(n)
			}
			if nk == termKey {
				hitTerm = true
			}
			if nk == langKey {
				hitLang = true
			}
		}
		if hitTerm && hitLang {
			return true
		}
		// term 本身是 catalog canonical 而 lang 是别名
		for _, et := range catalog.Expand(langKey) {
			if matching.TitleKey(et.Text) == termKey || matching.TitleKey(matching.AnalyzeTitle(et.Text).Base) == termKey {
				return true
			}
		}
	}
	// catalog：term 与 langKey 同族
	for _, et := range catalog.Expand(langKey) {
		ek := matching.TitleKey(matching.AnalyzeTitle(et.Text).Base)
		if ek == termKey || matching.TitleKey(et.Text) == termKey {
			return true
		}
	}
	return false
}

func langsAllCovered(needed []string, covered map[string]bool) bool {
	if len(needed) == 0 {
		return true
	}
	for _, k := range needed {
		if !covered[k] {
			return false
		}
	}
	return true
}

func queryHasSeasonHint(text string, season int) bool {
	if season <= 0 {
		return false
	}
	t := strings.TrimSpace(text)
	if strings.Contains(t, fmt.Sprintf("第%d季", season)) {
		return true
	}
	// 拉丁：末尾空格+季号
	return strings.HasSuffix(t, fmt.Sprintf(" %d", season))
}

func (s *MatchingSession) selectChapterProbes(q matching.Query, ranked []matching.Candidate, probed map[int]bool, chapters map[int]matching.ChapterDecision) []int {
	if len(ranked) == 0 {
		return nil
	}
	var out []int
	budgetLeft := sessionChapterProbeBudget - len(probed)
	if budgetLeft <= 0 {
		return nil
	}

	needsAbsolute := false
	for _, n := range q.Episodes {
		if n > 0 {
			// 若任一候选有 EpisodeCount 且越界，或全无 count，则可能需要绝对号证据
			needsAbsolute = true
			break
		}
	}

	for i, c := range ranked {
		if budgetLeft <= 0 {
			break
		}
		id := c.Subject.ID
		if id <= 0 || probed[id] {
			continue
		}
		if _, done := chapters[id]; done {
			continue
		}
		if c.NameScore < sessionNameScoreProbeMin {
			continue
		}
		// 可能进入前三
		inTop3 := i < 3
		nearAccept := c.NameScore >= sessionNameScoreAcceptMin &&
			c.Score >= sessionScoreAcceptMin-sessionAcceptDistance &&
			len(c.Blockers) == 0
		needExplain := false
		if needsAbsolute {
			epCount := c.Subject.EpisodeCount
			for _, n := range q.Episodes {
				if epCount > 0 && n > epCount {
					needExplain = true
					break
				}
				if epCount <= 0 && n > 0 {
					needExplain = true
					break
				}
			}
		}
		if inTop3 && (nearAccept || needExplain) {
			out = append(out, id)
			budgetLeft--
		}
	}
	return out
}

func (s *MatchingSession) probeChapters(group matching.Group, id int) (matching.ChapterDecision, error) {
	if err := s.ensureDetailed(id); err != nil {
		return matching.ChapterDecision{}, err
	}
	sub := s.pool[id]
	chs, err := s.provider.Chapters(s.ctx, id)
	if err != nil {
		return matching.ChapterDecision{}, err
	}
	known := true
	dec := matching.InterpretChapters(group.Files, sub, chs, known)
	return dec, nil
}

func (s *MatchingSession) ensureDetailed(id int) error {
	if id <= 0 {
		return fmt.Errorf("条目 ID 无效: %w", ErrMatchUpstream)
	}
	s.ensurePool()
	if sub, ok := s.pool[id]; ok && sub.Detailed {
		return nil
	}
	detailed, err := s.provider.Detail(s.ctx, id)
	if err != nil {
		return err
	}
	if detailed.ID != id || !detailed.Detailed {
		return fmt.Errorf("详情无效 id=%d: %w", id, ErrMatchUpstream)
	}
	s.mergeSubject(detailed)
	return nil
}
