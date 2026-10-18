package matching

import (
	"fmt"
	"sort"
)

// InterpretChapters 按 Confirmed/CountFit/Conflict/Unchecked 分类章节解释。
// 只将 Single 的原始正整数写入 StoredNumber。
// 先找远端 Ep 精确等于原始号的唯一章节，再找 Sort 精确相等的唯一章节。
func InterpretChapters(files []FileEvidence, subject Subject, chapters []Chapter, chaptersKnown bool) ChapterDecision {
	out := ChapterDecision{
		State:    ChapterUnchecked,
		Bindings: nil,
		Rejected: map[string]string{},
	}
	if !chaptersKnown {
		return out
	}

	epIndex := indexChaptersBy(chapters, true)
	sortIndex := indexChaptersBy(chapters, false)

	type pending struct {
		bind EpisodeBinding
	}
	var pendings []pending
	chapterUsers := map[int][]string{} // ChapterID → files
	numberUsers := map[int][]string{}  // StoredNumber → files

	hasRealNumber1 := false
	for _, f := range files {
		if f.Episode.Kind == NumberSingle && f.Episode.First == 1 {
			hasRealNumber1 = true
		}
	}

	for _, f := range files {
		name := f.Name
		ep := f.Episode

		switch ep.Kind {
		case NumberRange:
			out.Rejected[name] = "不支持区间编号，拒绝截断或取整"
			continue
		case NumberFraction:
			out.Rejected[name] = "不支持小数编号，拒绝截断或取整"
			continue
		case NumberSpecial:
			out.Rejected[name] = "不支持特别篇/第0话等非正整数编号"
			continue
		case NumberUnknown:
			if f.Media == KindPromo || subject.Media == KindPromo {
				out.Rejected[name] = "宣传性质内容不支持自动编号"
				continue
			}
			// Movie 明确且无真实编号：允许 StoredNumber=1（组内尚无真实 1 时）
			if (f.Media == KindMovie || subject.Media == KindMovie) && !hasRealNumber1 {
				bind := EpisodeBinding{
					FileName:     name,
					StoredNumber: 1,
					ChapterID:    0,
					Basis:        "movie",
				}
				// 若章节列表有 Ep/Sort=1 的唯一章，附上 ChapterID
				if ch, ok := uniqueExact(epIndex, 1); ok {
					bind.ChapterID = ch.ID
					bind.Basis = "ep"
				} else if ch, ok := uniqueExact(sortIndex, 1); ok {
					bind.ChapterID = ch.ID
					bind.Basis = "sort"
				}
				pendings = append(pendings, pending{bind: bind})
				numberUsers[1] = append(numberUsers[1], name)
				if bind.ChapterID != 0 {
					chapterUsers[bind.ChapterID] = append(chapterUsers[bind.ChapterID], name)
				}
				hasRealNumber1 = true // 占位后不再让第二个无编号电影占 1
				continue
			}
			// 无编号且非电影：不产生 binding，也不直接 Reject（由上层决定）
			continue
		case NumberSingle:
			n := ep.First
			if n <= 0 {
				out.Rejected[name] = "集号必须为正整数"
				continue
			}
			bind, reject := resolveSingleNumber(n, name, chapters, epIndex, sortIndex, subject)
			if reject != "" {
				out.Rejected[name] = reject
				continue
			}
			pendings = append(pendings, pending{bind: bind})
			numberUsers[n] = append(numberUsers[n], name)
			if bind.ChapterID != 0 {
				chapterUsers[bind.ChapterID] = append(chapterUsers[bind.ChapterID], name)
			}
		default:
			out.Rejected[name] = "未知编号形态"
		}
	}

	// 组内：多文件同 ChapterID 或同 StoredNumber → 相关全部 Rejected
	dupFiles := map[string]string{}
	for id, names := range chapterUsers {
		if len(names) > 1 {
			reason := fmt.Sprintf("多个文件指向同一章节ID %d", id)
			for _, n := range names {
				dupFiles[n] = reason
			}
		}
	}
	for num, names := range numberUsers {
		if len(names) > 1 {
			reason := fmt.Sprintf("组内多个文件得到相同StoredNumber %d", num)
			for _, n := range names {
				dupFiles[n] = reason
			}
		}
	}
	for n, reason := range dupFiles {
		out.Rejected[n] = reason
	}

	// 生成 Bindings（排除已 Rejected）
	for _, p := range pendings {
		if _, bad := out.Rejected[p.bind.FileName]; bad {
			continue
		}
		out.Bindings = append(out.Bindings, p.bind)
	}

	if len(out.Rejected) > 0 {
		out.State = ChapterConflict
		return out
	}

	hasConfirmed := false
	hasCountFit := false
	for _, b := range out.Bindings {
		switch b.Basis {
		case "ep", "sort":
			hasConfirmed = true
		case "count":
			hasCountFit = true
		case "movie":
			// movie 占位无实际章节时不算 Confirmed
			if b.ChapterID != 0 {
				hasConfirmed = true
			}
		}
	}

	if hasConfirmed {
		out.State = ChapterConfirmed
		return out
	}
	if hasCountFit {
		out.State = ChapterCountFit
		return out
	}
	// 章节已知但无任何可解释编号 → Conflict（驱动 Review）
	if hasAnyNumbering(files) {
		out.State = ChapterConflict
		if len(out.Rejected) == 0 {
			// 标记整体原因到第一个文件，便于诊断
			for _, f := range files {
				if f.Episode.Kind == NumberSingle && f.Episode.First > 0 {
					out.Rejected[f.Name] = "章节列表中无法解释该编号"
					break
				}
			}
		}
		return out
	}
	out.State = ChapterUnchecked
	return out
}

func hasAnyNumbering(files []FileEvidence) bool {
	for _, f := range files {
		switch f.Episode.Kind {
		case NumberSingle, NumberRange, NumberFraction, NumberSpecial:
			return true
		}
	}
	return false
}

func resolveSingleNumber(
	n int, name string, chapters []Chapter,
	epIndex, sortIndex map[float64][]Chapter, subject Subject,
) (EpisodeBinding, string) {
	bind := EpisodeBinding{
		FileName:     name,
		StoredNumber: n,
	}
	nf := float64(n)

	epHits := epIndex[nf]
	sortHits := sortIndex[nf]

	var chosen *Chapter
	var basis string

	if len(epHits) == 1 {
		ch := epHits[0]
		chosen = &ch
		basis = "ep"
	} else if len(epHits) > 1 {
		return bind, fmt.Sprintf("多个章节 Ep 等于 %d", n)
	}

	if len(sortHits) == 1 {
		ch := sortHits[0]
		if chosen != nil && chosen.ID != ch.ID {
			return bind, fmt.Sprintf("Ep与Sort指向不同章节（Ep→%d Sort→%d）", chosen.ID, ch.ID)
		}
		if chosen == nil {
			chosen = &ch
			basis = "sort"
		}
	} else if len(sortHits) > 1 {
		return bind, fmt.Sprintf("多个章节 Sort 等于 %d", n)
	}

	if chosen != nil {
		bind.ChapterID = chosen.ID
		bind.Basis = basis
		return bind, ""
	}

	// 无精确 Ep/Sort 命中：不得用 [minSort,maxSort] 包围判定
	if len(chapters) > 0 {
		// 列表非空但无此号：即便落在 min..max 之间也不接受
		return bind, fmt.Sprintf("章节列表中无 Ep/Sort 精确等于 %d（不接受区间包围）", n)
	}

	// 列表空但 chaptersKnown：尝试详情话数
	if subject.EpisodeCount > 0 {
		if n >= 1 && n <= subject.EpisodeCount {
			bind.Basis = "count"
			return bind, ""
		}
		return bind, fmt.Sprintf("编号 %d 超出可信话数 %d", n, subject.EpisodeCount)
	}
	return bind, "无章节列表且无可信话数，无法解释编号"
}

func indexChaptersBy(chapters []Chapter, byEp bool) map[float64][]Chapter {
	out := make(map[float64][]Chapter, len(chapters))
	for _, ch := range chapters {
		var key float64
		if byEp {
			key = ch.Ep
		} else {
			key = ch.Sort
		}
		out[key] = append(out[key], ch)
	}
	return out
}

func uniqueExact(index map[float64][]Chapter, n int) (Chapter, bool) {
	hits := index[float64(n)]
	if len(hits) == 1 {
		return hits[0], true
	}
	return Chapter{}, false
}

// CheckBindingConflicts 同目录+subject 跨组/既有行查重。
// 多个新文件冲突 → 相关新文件全部进入冲突；既有 vs 新 → 保留既有、拒绝新文件。
// 不以遍历顺序选赢家。现存行彼此冲突仅忽略（不修改全库）。
func CheckBindingConflicts(batches []BindingBatch) []BindingConflict {
	type key struct {
		dir string
		sid int
	}
	type ref struct {
		file     string
		existing bool
		number   int
		chapter  int
	}

	byKey := map[key][]ref{}
	for _, b := range batches {
		k := key{dir: b.Dir, sid: b.SubjectID}
		for _, bind := range b.Bindings {
			byKey[k] = append(byKey[k], ref{
				file:     bind.FileName,
				existing: b.Existing,
				number:   bind.StoredNumber,
				chapter:  bind.ChapterID,
			})
		}
	}

	var conflicts []BindingConflict
	// 稳定输出：按 dir、sid 排序
	keys := make([]key, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].dir != keys[j].dir {
			return keys[i].dir < keys[j].dir
		}
		return keys[i].sid < keys[j].sid
	})

	for _, k := range keys {
		refs := byKey[k]
		// StoredNumber 查重
		byNum := map[int][]ref{}
		byCh := map[int][]ref{}
		for _, r := range refs {
			if r.number > 0 {
				byNum[r.number] = append(byNum[r.number], r)
			}
			if r.chapter != 0 {
				byCh[r.chapter] = append(byCh[r.chapter], r)
			}
		}
		seenFiles := map[string]struct{}{}
		addConflict := func(files []string, reason string) {
			if len(files) == 0 {
				return
			}
			sort.Strings(files)
			conflicts = append(conflicts, BindingConflict{
				Dir:       k.dir,
				SubjectID: k.sid,
				Files:     files,
				Reason:    reason,
			})
		}

		collect := func(group []ref, reason string) {
			var newFiles []string
			var existingFiles []string
			for _, r := range group {
				if r.existing {
					existingFiles = append(existingFiles, r.file)
				} else {
					newFiles = append(newFiles, r.file)
				}
			}
			if len(group) <= 1 {
				return
			}
			// 仅现存行彼此冲突 → 记而不改（不输出为需拒绝的新冲突）
			if len(newFiles) == 0 {
				return
			}
			if len(existingFiles) > 0 {
				// 既有 vs 新：拒绝新文件
				filtered := uniqueFileNames(newFiles, seenFiles)
				addConflict(filtered, reason+"（保留既有行，拒绝新文件）")
				return
			}
			// 多个新文件
			filtered := uniqueFileNames(newFiles, seenFiles)
			addConflict(filtered, reason)
		}

		numKeys := sortedIntKeys(byNum)
		for _, n := range numKeys {
			collect(byNum[n], fmt.Sprintf("StoredNumber %d 重复", n))
		}
		chKeys := sortedIntKeys(byCh)
		for _, id := range chKeys {
			collect(byCh[id], fmt.Sprintf("ChapterID %d 重复", id))
		}
	}
	if conflicts == nil {
		return []BindingConflict{}
	}
	return conflicts
}

func uniqueFileNames(files []string, seen map[string]struct{}) []string {
	var out []string
	for _, f := range files {
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return out
}

func sortedIntKeys[T any](m map[int]T) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}
