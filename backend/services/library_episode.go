package services

import (
	"math"
	"strings"

	"fan-web/database"
	"fan-web/models"
	"fan-web/services/matching"
)

// libraryEpisodeNum 把解析结果规范成入库/单番扫描用的集号。
// Kind=="movie" 返回 1，否则返回 EpisodeNum。纯函数，Scan 与 Scanner 共用。
func libraryEpisodeNum(p ParsedFilename) int {
	if p.Kind == "movie" {
		return 1
	}
	return p.EpisodeNum
}

func subjectAllowsEpisodeOne(subject *BangumiSubjectInfo, parsed ParsedFilename) bool {
	if subject != nil && subject.TotalEpisodes == 1 {
		return true
	}
	if subject != nil && isMoviePlatform(subject.Platform) {
		return true
	}
	if parsed.Kind == "movie" && (subject == nil || subject.TotalEpisodes <= 1) {
		return true
	}
	return false
}

func isMoviePlatform(platform string) bool {
	if strings.Contains(platform, "剧场版") || strings.Contains(platform, "电影") {
		return true
	}
	return strings.Contains(strings.ToLower(platform), "movie")
}

// episodeCeiling 计算条目允许的最大集号。纯函数。
// 返回 max(subject.TotalEpisodes, ceil(episodes 中最大 Sort))；
// subject 可为 nil；两者都未知（<=0）时返回 0 = 无法判定（调用方放行）。
// episodes 为 nil/空 时只用 subject.TotalEpisodes。
func episodeCeiling(subject *BangumiSubjectInfo, episodes []BangumiEpisode) int {
	base := 0
	if subject != nil && subject.TotalEpisodes > 0 {
		base = subject.TotalEpisodes
	}
	if len(episodes) == 0 {
		return base
	}
	maxSort := episodes[0].Sort
	for _, ep := range episodes[1:] {
		if ep.Sort > maxSort {
			maxSort = ep.Sort
		}
	}
	sortCeil := int(math.Ceil(maxSort))
	if sortCeil > base {
		return sortCeil
	}
	return base
}

// subjectFromBoundAnime 目录绑定快通道用的 Subject 视图；Detailed=false。
func subjectFromBoundAnime(anime models.Anime) matching.Subject {
	return matching.Subject{
		ID:           anime.BangumiID,
		Name:         anime.Title,
		NameCn:       anime.TitleCn,
		EpisodeCount: anime.EpCount,
		Cover:        anime.Cover,
		Summary:      anime.Summary,
		Detailed:     false,
	}
}

// matchCandidatesFromDecision 将内核候选转为收件箱 JSON（最多 3 条，非 nil）。
func matchCandidatesFromDecision(d matching.Decision) []MatchCandidate {
	out := make([]MatchCandidate, 0, 3)
	for i, c := range d.Candidates {
		if i >= 3 {
			break
		}
		out = append(out, MatchCandidate{
			ID:     c.Subject.ID,
			Name:   c.Subject.Name,
			NameCn: c.Subject.NameCn,
			Score:  c.Score,
		})
	}
	return out
}

// libraryExistingBindingBatches 目录内既有剧集（磁盘仍在）作为 Existing 批次，供跨组查重。
// ChapterID 未知时仅按 StoredNumber 检查，不伪造章节。
func libraryExistingBindingBatches(dir string, presentFiles []string) ([]matching.BindingBatch, error) {
	present := make(map[string]struct{}, len(presentFiles))
	for _, name := range presentFiles {
		present[name] = struct{}{}
	}
	animes, err := database.ListAnimesByFilePath(dir)
	if err != nil {
		return nil, err
	}
	batches := make([]matching.BindingBatch, 0, len(animes))
	for _, anime := range animes {
		if anime.BangumiID <= 0 {
			continue
		}
		eps, err := database.ListEpisodesByAnimeID(anime.ID)
		if err != nil {
			return nil, err
		}
		binds := make([]matching.EpisodeBinding, 0, len(eps))
		for _, ep := range eps {
			if _, ok := present[ep.FilePath]; !ok {
				continue
			}
			if ep.EpNumber <= 0 {
				continue
			}
			binds = append(binds, matching.EpisodeBinding{
				FileName:     ep.FilePath,
				StoredNumber: ep.EpNumber,
			})
		}
		if len(binds) == 0 {
			continue
		}
		batches = append(batches, matching.BindingBatch{
			Dir:       dir,
			SubjectID: anime.BangumiID,
			Existing:  true,
			Bindings:  binds,
		})
	}
	return batches, nil
}
