package services

import (
	"context"
	"fmt"

	"fan-web/services/matching"
)

// MatchingSession 本次操作独享缓存、预算、降级状态。
// S0 仅端口骨架；Resolve 行为属 S5。
type MatchingSession struct {
	runtime *MatchingRuntime
	ctx     context.Context
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

// Resolve 对分组做自动匹配。S0 骨架返回明确未实现错误。
func (s *MatchingSession) Resolve(group matching.Group, bound *BoundMatch) (MatchResult, error) {
	_ = group
	_ = bound
	if s == nil {
		return emptyMatchResult(), fmt.Errorf("MatchingSession.Resolve: session 为空")
	}
	return emptyMatchResult(), fmt.Errorf("MatchingSession.Resolve: 尚未实现（S5）")
}

// ResolveForTarget 先做相同自动判断，再以明确目标确认。S0 骨架返回明确未实现错误。
func (s *MatchingSession) ResolveForTarget(group matching.Group, target matching.Subject) (MatchResult, error) {
	_ = group
	_ = target
	if s == nil {
		return emptyMatchResult(), fmt.Errorf("MatchingSession.ResolveForTarget: session 为空")
	}
	return emptyMatchResult(), fmt.Errorf("MatchingSession.ResolveForTarget: 尚未实现（S5）")
}
