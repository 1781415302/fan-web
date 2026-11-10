package matching

import "fmt"

// fileExtras 预抽取证据；由各 pass 写入，经 mergeExtras 并入 FileEvidence。
type fileExtras struct {
	Season      int // 0=未识别
	Episode     int // 单集号；0=未识别
	EpisodeLast int // 区间末集（Episode..EpisodeLast）；0 或等于 Episode 表示单集
	Abs         int // 总集数标注（[总第85]）
	Part        int
	Final       bool
	Notes       []string // 供测试/诊断，不进 Conflicts
}

func preExtract(stem string) (string, fileExtras) {
	var ex fileExtras
	stem = extractEpisodeTokens(stem, &ex) // S1
	stem = extractFansubMeta(stem, &ex)    // S2
	stem = extractOrdinalWords(stem, &ex)  // S2
	return stem, ex
}

// mergeExtras 将预抽取证据并入正解析结果。规则见契约 4.0。
func mergeExtras(ev *FileEvidence, ex fileExtras) {
	if ev == nil {
		return
	}
	if ex.Episode > 0 {
		if isUnknownEp(ev.Episode) {
			if ex.EpisodeLast > 0 && ex.EpisodeLast != ex.Episode {
				ev.Episode = EpisodeToken{
					Kind:     NumberRange,
					First:    ex.Episode,
					Last:     ex.EpisodeLast,
					Explicit: true,
				}
			} else {
				ev.Episode = EpisodeToken{
					Kind:     NumberSingle,
					First:    ex.Episode,
					Last:     ex.Episode,
					Explicit: true,
				}
			}
		} else if extrasEpisodeDiffers(ev.Episode, ex) {
			ev.Conflicts = append(ev.Conflicts, fmt.Sprintf("预抽取集号%d与正文集号%d冲突", ex.Episode, ev.Episode.First))
		}
	}
	if ex.Season > 0 {
		if ev.Ordinal.Season == 0 {
			ev.Ordinal.Season = ex.Season
		} else if ev.Ordinal.Season != ex.Season {
			ev.Conflicts = append(ev.Conflicts, "季号冲突")
		}
	}
	if ex.Abs > 0 {
		ev.AbsEpisode = ex.Abs
	}
	if ex.Part > 0 && ev.Ordinal.Part == 0 {
		ev.Ordinal.Part = ex.Part
	}
	if ex.Final {
		ev.Ordinal.Final = true
	}
}

func extrasEpisodeDiffers(ep EpisodeToken, ex fileExtras) bool {
	exLast := ex.Episode
	if ex.EpisodeLast > 0 {
		exLast = ex.EpisodeLast
	}
	return ep.First != ex.Episode || ep.Last != exLast
}
