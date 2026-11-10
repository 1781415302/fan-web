package matching

import "math"

// ComputeSeasonAnchor 各 sibling 章节最小 Sort 的全局最小；空/无正值→0。
// 只统计 Sort>0 的章节；纯函数，不修改入参。
func ComputeSeasonAnchor(siblingChapters map[int][]Chapter) int {
	anchor := 0
	for _, chs := range siblingChapters {
		for _, ch := range chs {
			if ch.Sort <= 0 {
				continue
			}
			v := int(math.Ceil(ch.Sort))
			if float64(v) != ch.Sort {
				// 非整数 Sort（如 12.5 总集篇）不作为锚点
				continue
			}
			if anchor == 0 || v < anchor {
				anchor = v
			}
		}
	}
	return anchor
}

// ResolveAbsOrAnchor 在文件号 n 的原始 Ep/Sort 精确匹配失败后调用（纯函数）：
//  1. absEpisode>0 且存在唯一 chapter.Sort==absEpisode → basis "abs_sort"；
//  2. opts.SeasonSortAnchor>0、n>0 且存在唯一 chapter.Sort==anchor+n-1 → basis "season_sort"；
//  3. 否则 ok=false（由调用方保持原拒绝文案）。
//
// 命中章节的 Ep 必须为 >=1 的整数（StoredNumber=int(chapter.Ep)），否则视为该步未命中。
func ResolveAbsOrAnchor(n, absEpisode int, chapters []Chapter, opts ChapterOptions) (Chapter, string, bool) {
	if absEpisode > 0 {
		if ch, ok := uniqueSortWithEp(chapters, absEpisode); ok {
			return ch, "abs_sort", true
		}
	}
	if opts.SeasonSortAnchor > 0 && n > 0 {
		if ch, ok := uniqueSortWithEp(chapters, opts.SeasonSortAnchor+n-1); ok {
			return ch, "season_sort", true
		}
	}
	return Chapter{}, "", false
}

// SubjectSeason 导出条目季号（与 Rank 内部 subjectOrdinal 同一语义），供 services 包计算兄弟集合。
func SubjectSeason(sub Subject) int {
	return subjectOrdinal(sub).Season
}

// uniqueSortWithEp：唯一 chapter.Sort==target，且其 Ep 为 >=1 的整数。
func uniqueSortWithEp(chapters []Chapter, target int) (Chapter, bool) {
	var hit Chapter
	count := 0
	for _, ch := range chapters {
		if ch.Sort == float64(target) {
			hit = ch
			count++
		}
	}
	if count != 1 {
		return Chapter{}, false
	}
	if hit.Ep < 1 || hit.Ep != math.Trunc(hit.Ep) {
		return Chapter{}, false
	}
	return hit, true
}
