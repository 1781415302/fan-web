package services

import "fan-web/services/matching"

// anchorSiblingMax 计算季锚点时最多取的兄弟条目数（含目标，目标优先）。
const anchorSiblingMax = 3

// interpretWithAnchor 在章节解释中附加季锚点兜底。
// 先按零值选项解释；仅当存在被拒文件（锚点兜底可能改变结果）时才取兄弟章节计算锚点，
// 避免为已全部命中的组多发章节请求。锚点只作用于原 Ep/Sort 无命中的文件，
// 因此零值结果无被拒文件时与带锚点结果逐字节相同。
func (s *MatchingSession) interpretWithAnchor(group matching.Group, sub matching.Subject, chs []matching.Chapter, q matching.Query, ranked []matching.Candidate) matching.ChapterDecision {
	base := matching.InterpretChaptersWith(group.Files, sub, chs, true, matching.ChapterOptions{})
	if len(base.Rejected) == 0 || len(chs) == 0 {
		return base
	}
	anchor := s.seasonSortAnchor(sub, chs, q, ranked)
	if anchor <= 0 {
		return base
	}
	return matching.InterpretChaptersWith(group.Files, sub, chs, true, matching.ChapterOptions{SeasonSortAnchor: anchor})
}

// seasonSortAnchor 计算同季兄弟集合的最小 Sort：
// siblings = {目标} ∪ {ranked 中 NameScore>=0.88、无 Blockers、series、条目季号==q 季号>0 的候选}，最多 anchorSiblingMax 个。
// 兄弟章节经 provider（缓存/会话预算）获取；任一获取失败则忽略该兄弟（仍占名额）；全部无章节或 q 无季号 → 0。
func (s *MatchingSession) seasonSortAnchor(sub matching.Subject, chs []matching.Chapter, q matching.Query, ranked []matching.Candidate) int {
	season := q.Ordinal.Season
	if season <= 0 {
		return 0
	}
	siblingChapters := map[int][]matching.Chapter{sub.ID: chs}
	taken := 1 // 目标优先占一个名额；获取失败的兄弟同样占名额，限制额外请求数
	for _, c := range ranked {
		if taken >= anchorSiblingMax {
			break
		}
		id := c.Subject.ID
		if id <= 0 || id == sub.ID {
			continue
		}
		if _, done := siblingChapters[id]; done {
			continue
		}
		if c.NameScore < sessionNameScoreAcceptMin || len(c.Blockers) != 0 {
			continue
		}
		if c.Subject.Media != matching.KindSeries {
			continue
		}
		if matching.SubjectSeason(c.Subject) != season {
			continue
		}
		if s.isUnavailable(id) {
			continue
		}
		taken++
		sibChs, err := s.provider.Chapters(s.ctx, id)
		if err != nil {
			// 兄弟章节获取失败：忽略该兄弟，不向上抛错
			continue
		}
		siblingChapters[id] = sibChs
	}
	return matching.ComputeSeasonAnchor(siblingChapters)
}
