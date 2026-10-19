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

func firstNonExcluded(cs []Candidate) *Candidate {
	for i := range cs {
		// Missing 与 Blockers 均不可作为自动接受的次优竞争者（避免「无法解释季号」同分卡住胜者）
		if len(cs[i].Blockers) == 0 && len(cs[i].Missing) == 0 {
			return &cs[i]
		}
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
