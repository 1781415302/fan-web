package services

import "fan-web/services/matching"

// interpretWithAnchor 在章节解释中附加季锚点兜底。
// Stage 0 为恒等桩：行为与 InterpretChapters(..., true) 相同。
func (s *MatchingSession) interpretWithAnchor(group matching.Group, sub matching.Subject, chs []matching.Chapter, q matching.Query, ranked []matching.Candidate) matching.ChapterDecision {
	return matching.InterpretChapters(group.Files, sub, chs, true)
}
