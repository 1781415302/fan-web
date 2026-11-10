package matching

// ShiftGroupEpisodes 对 g.Files 中 Episode.Kind==NumberSingle 的文件把 First/Last 同减 offset，
// 随后用 buildGroupQuery 重建 g.Query（Query.Episodes / Query.Conflicts 随之刷新，
// selectChapterProbes 与 Decide 读的是 Query 而非 Files）。
// 偏移后 <=0 的文件：Episode 置为 Unknown 并追加 Files[i].Conflicts "集号偏移后<=0"
// （重建后进入 q.Conflicts → Decide 走 Review）。
// 非 Single（Range/Special/Fraction）不平移；被平移文件的 AbsEpisode 置 0。offset<=0 → 原样返回。
// 纯函数：不修改入参 g 的 Files 底层数组。
func ShiftGroupEpisodes(g Group, offset int) Group {
	if offset <= 0 {
		return g
	}
	files := make([]FileEvidence, len(g.Files))
	for i, f := range g.Files {
		if f.Conflicts != nil {
			f.Conflicts = append([]string(nil), f.Conflicts...)
		}
		if f.Episode.Kind == NumberSingle {
			first := f.Episode.First - offset
			last := f.Episode.Last - offset
			if first <= 0 || last <= 0 {
				f.Episode = EpisodeToken{Kind: NumberUnknown, Raw: f.Episode.Raw}
				f.Conflicts = append(f.Conflicts, "集号偏移后<=0")
			} else {
				f.Episode.First = first
				f.Episode.Last = last
			}
			f.AbsEpisode = 0
		}
		files[i] = f
	}
	out := g
	out.Files = files
	out.Query = buildGroupQuery(files)
	return out
}
