package matching

import (
	"fmt"
	"sort"
	"strings"
)

// GroupFiles 只做纯分组，不调用 Provider/数据库。
// 分组身份：相对目录、严格的首个可用文件标题 base、已知 Year、Ordinal 三项、Media。
// 未知值形成独立组；unknown 不作通配。
func GroupFiles(inputs []FileInput) []Group {
	type acc struct {
		key   string
		dir   string
		files []FileEvidence
		order int
	}
	groups := map[string]*acc{}
	var order []string

	for i, in := range inputs {
		ev := ParseFile(in)
		key := groupIdentityKey(in.Dir, ev)
		g, ok := groups[key]
		if !ok {
			g = &acc{key: key, dir: in.Dir, order: i}
			groups[key] = g
			order = append(order, key)
		}
		g.files = append(g.files, ev)
	}

	// Stable: first-seen key order; files keep encounter order
	sort.SliceStable(order, func(i, j int) bool {
		return groups[order[i]].order < groups[order[j]].order
	})

	out := make([]Group, 0, len(order))
	for _, k := range order {
		g := groups[k]
		out = append(out, Group{
			Key:   g.key,
			Dir:   g.dir,
			Files: g.files,
			Query: buildGroupQuery(g.files),
		})
	}
	return out
}

func groupIdentityKey(dir string, ev FileEvidence) string {
	base := firstFileTitleBase(ev)
	// Strict base key
	baseKey := Normalize(base).Strict
	return fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d\x00%t\x00%s",
		dir,
		baseKey,
		ev.Year,
		ev.Ordinal.Season,
		ev.Ordinal.Part,
		ev.Ordinal.Final,
		string(ev.Media),
	)
}

func firstFileTitleBase(ev FileEvidence) string {
	for _, t := range ev.Titles {
		if t.Source != SourceFile && t.Source != SourceDirectory {
			continue
		}
		if strings.TrimSpace(t.Text) == "" {
			continue
		}
		tf := AnalyzeTitle(t.Text)
		if tf.Base != "" {
			return tf.Base
		}
		return t.Text
	}
	return ""
}

func buildGroupQuery(files []FileEvidence) Query {
	q := Query{
		Media:    KindUnknown,
		Episodes: []int{},
	}
	titleSeen := map[string]bool{}
	var seasons, parts []int
	var finals []bool
	var years []int
	var medias []MediaKind
	hasFileTitle := false
	var conflicts []string

	for _, f := range files {
		for _, t := range f.Titles {
			if t.Source == SourceFile {
				hasFileTitle = true
			}
			k := TitleKey(t.Text) + "|" + string(t.Source)
			if !titleSeen[k] {
				titleSeen[k] = true
				q.Titles = append(q.Titles, t)
			}
		}
		if f.Ordinal.Season > 0 {
			seasons = append(seasons, f.Ordinal.Season)
		}
		if f.Ordinal.Part > 0 {
			parts = append(parts, f.Ordinal.Part)
		}
		finals = append(finals, f.Ordinal.Final)
		if f.Year > 0 {
			years = append(years, f.Year)
		}
		if f.Media != KindUnknown {
			medias = append(medias, f.Media)
		}
		if f.Episode.Kind == NumberSingle && f.Episode.First > 0 {
			q.Episodes = append(q.Episodes, f.Episode.First)
		}
		conflicts = append(conflicts, f.Conflicts...)
	}

	q.HasFileTitle = hasFileTitle
	q.Conflicts = uniqueStrings(conflicts)
	q.Episodes = uniqueSortedInts(q.Episodes)

	if u := uniquePositive(seasons); len(u) == 1 {
		q.Ordinal.Season = u[0]
	} else if len(u) > 1 {
		q.Conflicts = append(q.Conflicts, fmt.Sprintf("组内季号冲突：%v", u))
	}
	if u := uniquePositive(parts); len(u) == 1 {
		q.Ordinal.Part = u[0]
	}
	for _, f := range finals {
		if f {
			q.Ordinal.Final = true
			break
		}
	}
	if u := uniquePositive(years); len(u) == 1 {
		q.Year = u[0]
	} else if len(u) > 1 {
		q.Conflicts = append(q.Conflicts, fmt.Sprintf("组内年份冲突：%v", u))
	}
	mediaU := uniqueMedia(medias)
	if len(mediaU) == 1 {
		q.Media = mediaU[0]
	} else if len(mediaU) > 1 {
		q.Conflicts = append(q.Conflicts, "组内媒体类型冲突")
	}
	if q.Titles == nil {
		q.Titles = []Title{}
	}
	if q.Episodes == nil {
		q.Episodes = []int{}
	}
	if q.Conflicts == nil {
		q.Conflicts = []string{}
	}
	return q
}

func uniqueSortedInts(xs []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, x := range xs {
		if x <= 0 || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	sort.Ints(out)
	return out
}

func uniqueMedia(xs []MediaKind) []MediaKind {
	seen := map[MediaKind]bool{}
	var out []MediaKind
	for _, x := range xs {
		if x == KindUnknown || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}
