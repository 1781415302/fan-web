package matching

import (
	"fmt"
	"strings"
)

// Decide 自动三态决策。retrievalComplete=false 时不得自动接受。
func Decide(q Query, ranked []Candidate, retrievalComplete bool) Decision {
	cands := topCandidates(ranked, 3)
	if len(ranked) == 0 {
		return Decision{
			Code:       NoCandidates,
			Candidates: nonNilCandidates(nil),
			Reason:     "无候选",
		}
	}
	if len(q.Conflicts) > 0 {
		return Decision{
			Code:       Review,
			Candidates: cands,
			Reason:     "查询存在明确冲突：" + strings.Join(q.Conflicts, "；"),
		}
	}
	best := ranked[0]
	if !retrievalComplete {
		return Decision{
			Code:       Review,
			Candidates: cands,
			Reason:     "召回不完整，不能自动接受",
		}
	}
	if len(best.Blockers) > 0 {
		return Decision{
			Code:       Review,
			Candidates: cands,
			Reason:     "存在明确阻断：" + strings.Join(best.Blockers, "；"),
		}
	}
	if len(best.Missing) > 0 {
		return Decision{
			Code:       Review,
			Candidates: cands,
			Reason:     "缺少必要证据：" + strings.Join(best.Missing, "；"),
		}
	}
	if best.NameScore < nameScoreAcceptMin || best.Score < scoreAcceptMin {
		return Decision{
			Code:       Review,
			Candidates: cands,
			Reason: fmt.Sprintf("分数未达自动接受阈值（NameScore=%.3f Score=%.3f）",
				best.NameScore, best.Score),
		}
	}
	second := firstNonExcluded(ranked[1:])
	if second != nil && best.Score-second.Score < scoreGapMin {
		return Decision{
			Code:       Review,
			Candidates: cands,
			Reason: fmt.Sprintf("与次优可用候选分差不足（%.3f < %.2f）",
				best.Score-second.Score, scoreGapMin),
		}
	}
	return Decision{
		Code:       Accepted,
		WinnerID:   best.Subject.ID,
		Candidates: cands,
		Reason:     "满足自动接受条件",
	}
}

// ConfirmTarget 显式用户目标确认政策（仅单番 POST Scan）。
// 可消解纯名称低分/分差/未知季的 Missing；不能忽略 Blockers。
// 自动决定已接受其他 subject 时不覆盖。
func ConfirmTarget(q Query, ranked []Candidate, targetID int) Decision {
	cands := topCandidates(ranked, 3)
	if len(ranked) == 0 || targetID <= 0 {
		return Decision{
			Code:       NoCandidates,
			Candidates: nonNilCandidates(cands),
			Reason:     "无候选或无效目标，不能凭空确认",
		}
	}

	// 自动已接受其他作品 → 不覆盖
	auto := Decide(q, ranked, true)
	if auto.Code == Accepted && auto.WinnerID != targetID {
		auto.Reason = "自动决定已接受其他作品，不覆盖为目标"
		return auto
	}
	if auto.Code == Accepted && auto.WinnerID == targetID {
		return auto
	}

	var target *Candidate
	targetRank := -1
	for i := range ranked {
		if ranked[i].Subject.ID == targetID {
			target = &ranked[i]
			targetRank = i
			break
		}
	}
	if target == nil {
		return Decision{
			Code:       Review,
			Candidates: cands,
			Reason:     fmt.Sprintf("目标 %d 不在候选池", targetID),
		}
	}
	if targetRank >= 3 {
		return Decision{
			Code:       Review,
			Candidates: cands,
			Reason:     fmt.Sprintf("目标 %d 不在 Rank Top3", targetID),
		}
	}
	if target.NameScore < confirmNameScoreMin {
		return Decision{
			Code:       Review,
			Candidates: cands,
			Reason: fmt.Sprintf("目标身份分过低（NameScore=%.3f < %.2f），不能强制确认",
				target.NameScore, confirmNameScoreMin),
		}
	}
	if len(target.Blockers) > 0 {
		return Decision{
			Code:       Review,
			Candidates: cands,
			Reason:     "目标存在明确冲突，不能确认：" + strings.Join(target.Blockers, "；"),
		}
	}
	// 章节可解释：ChapterConflict 已体现为 Blockers；此处再保险
	for _, b := range target.Blockers {
		if strings.Contains(b, "章节") {
			return Decision{
				Code:       Review,
				Candidates: cands,
				Reason:     "章节不可解释：" + b,
			}
		}
	}
	// Missing 中的硬章节/不支持编号已不应出现在无 Blockers 路径；
	// 纯名称低分/分差/未知季的 Missing 可消解。
	return Decision{
		Code:       Accepted,
		WinnerID:   targetID,
		Candidates: cands,
		Reason:     "用户显式确认目标",
	}
}

// ordinalStructureMissing 为「查询有序数要求、候选无法解释」的缺证。
// 这类候选不能自动接受，也不应挡住已能解释该序数的竞争者；
// 但身份类 Missing（并列语言片段等）必须继续参与分差竞争。
func ordinalStructureMissing(m string) bool {
	switch m {
	case "候选无法解释查询季号", "候选无法解释Part", "候选无法解释Final":
		return true
	default:
		return false
	}
}

// OnlyOrdinalStructureMissing 报告候选的 Missing 是否全部为序数结构缺证（无身份歧义类）。
func OnlyOrdinalStructureMissing(c Candidate) bool {
	if len(c.Missing) == 0 {
		return false
	}
	for _, m := range c.Missing {
		if !ordinalStructureMissing(m) {
			return false
		}
	}
	return true
}

// PreferEligibleOrder 稳定重排，不改分数：
//  1. 无 Blockers 且非「仅序数结构 Missing」（含完全可证与身份类 Missing）— 保留相对分序；
//  2. 仅序数结构 Missing；
//  3. 带 Blockers。
//
// 这样「无法解释季号」不会因 ID 升序卡在可接受者前，
// 同时身份歧义的高分候选不会被沉到近似第三作之后。
func PreferEligibleOrder(ranked []Candidate) []Candidate {
	if len(ranked) < 2 {
		return ranked
	}
	var primary, structMiss, blocked []Candidate
	for _, c := range ranked {
		switch {
		case len(c.Blockers) > 0:
			blocked = append(blocked, c)
		case OnlyOrdinalStructureMissing(c):
			structMiss = append(structMiss, c)
		default:
			primary = append(primary, c)
		}
	}
	if len(structMiss) == 0 && len(blocked) == 0 {
		return ranked
	}
	if len(primary) == len(ranked) {
		return ranked
	}
	out := make([]Candidate, 0, len(ranked))
	out = append(out, primary...)
	out = append(out, structMiss...)
	out = append(out, blocked...)
	return out
}

func firstNonExcluded(cs []Candidate) *Candidate {
	for i := range cs {
		if len(cs[i].Blockers) > 0 {
			continue
		}
		// 仅序数结构缺证可排除出分差；身份类 Missing 必须参与，避免近似第三作绕过歧义。
		if OnlyOrdinalStructureMissing(cs[i]) {
			continue
		}
		return &cs[i]
	}
	return nil
}

func topCandidates(ranked []Candidate, n int) []Candidate {
	if n <= 0 {
		return nonNilCandidates(nil)
	}
	if len(ranked) < n {
		n = len(ranked)
	}
	out := make([]Candidate, n)
	copy(out, ranked[:n])
	return nonNilCandidates(out)
}

func nonNilCandidates(cs []Candidate) []Candidate {
	if cs == nil {
		return []Candidate{}
	}
	return cs
}
