package services

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	bangumiMatchMinScore = 0.86
	bangumiMatchMinGap   = 0.18
	bangumiMatchMaxCands = 3
)

type BangumiMatchDecision struct {
	Accept     bool
	Winner     *BangumiSearchItem
	Candidates []MatchCandidate
}

type MatchCandidate struct {
	ID     int     `json:"id"`
	Name   string  `json:"name"`
	NameCn string  `json:"name_cn"`
	Score  float64 `json:"score"`
}

var matchBracketsRe = regexp.MustCompile(`[\[\(\{（【「『《〈][^\]\)\}）】」』》〉]*[\]\)\}）】」』》〉]`)

var matchSeasonRe = regexp.MustCompile(`\b\d+(?:st|nd|rd|th)\s*season\b|\bseason\s*\d+\b|\bs\d{1,2}\b|第\s*[0-9一二三四五六七八九十百]+\s*季`)

// DecideBangumiMatch scores Bangumi search hits against the original title
// (never a shortened query) and accepts only a clear unique winner.
func DecideBangumiMatch(originalTitle string, items []BangumiSearchItem) BangumiMatchDecision {
	if strings.TrimSpace(originalTitle) == "" || len(items) == 0 {
		return BangumiMatchDecision{Accept: false}
	}

	normOrig := normalizeTitle(originalTitle)
	querySeason := extractSeason(originalTitle)
	scored := make([]scoredItem, 0, len(items))
	for i, item := range items {
		scored = append(scored, scoredItem{
			idx:   i,
			score: itemMatchScore(normOrig, querySeason, item),
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	best := scored[0].score
	second := 0.0
	if len(scored) > 1 {
		second = scored[1].score
	}

	origRunes := utf8.RuneCountInString(normOrig)
	if origRunes >= 2 && best >= bangumiMatchMinScore && (best-second) >= bangumiMatchMinGap {
		return BangumiMatchDecision{
			Accept: true,
			Winner: &items[scored[0].idx],
		}
	}

	n := bangumiMatchMaxCands
	if n > len(scored) {
		n = len(scored)
	}
	candidates := make([]MatchCandidate, 0, n)
	for _, s := range scored[:n] {
		item := items[s.idx]
		candidates = append(candidates, MatchCandidate{
			ID:     item.ID,
			Name:   item.Name,
			NameCn: item.NameCn,
			Score:  s.score,
		})
	}
	return BangumiMatchDecision{
		Accept:     false,
		Candidates: candidates,
	}
}

type scoredItem struct {
	idx   int
	score float64
}

func itemMatchScore(normOrig string, querySeason int, item BangumiSearchItem) float64 {
	nameScore := diceBigram(normOrig, normalizeTitle(item.Name))
	cnScore := diceBigram(normOrig, normalizeTitle(item.NameCn))
	score := nameScore
	if cnScore > nameScore {
		score = cnScore
	}

	candSeason, unnamed := candidateSeason(item)
	if querySeason == 0 {
		return score
	}
	if querySeason == 1 && unnamed {
		return score
	}
	if querySeason > 0 && querySeason != candSeason {
		if score > 0.85 {
			score = 0.85
		}
	}
	return score
}

func candidateSeason(item BangumiSearchItem) (season int, unnamed bool) {
	season = extractSeason(item.Name)
	if season == 0 {
		season = extractSeason(item.NameCn)
	}
	if season == 0 {
		return 1, true
	}
	return season, false
}

func extractSeason(s string) int {
	token := matchSeasonRe.FindString(strings.ToLower(s))
	if token == "" {
		return 0
	}
	return parseSeasonToken(token)
}

func parseSeasonToken(token string) int {
	token = strings.ToLower(strings.TrimSpace(token))
	if n := firstASCIIDigits(token); n > 0 {
		return n
	}
	cn := chineseNumRe.FindString(token)
	if cn == "" {
		return 0
	}
	return parseChineseNum(cn)
}

func firstASCIIDigits(s string) int {
	n := 0
	found := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			found = true
			n = n*10 + int(r-'0')
			continue
		}
		if found {
			break
		}
	}
	return n
}

var chineseNumRe = regexp.MustCompile(`[一二三四五六七八九十百]+`)

func parseChineseNum(s string) int {
	digits := map[rune]int{
		'一': 1, '二': 2, '三': 3, '四': 4, '五': 5,
		'六': 6, '七': 7, '八': 8, '九': 9,
	}
	if s == "十" {
		return 10
	}
	runes := []rune(s)
	if len(runes) == 1 {
		return digits[runes[0]]
	}
	if runes[0] == '十' {
		if len(runes) == 2 {
			return 10 + digits[runes[1]]
		}
		return 0
	}
	if len(runes) >= 2 && runes[1] == '十' {
		tens := digits[runes[0]]
		if tens == 0 {
			return 0
		}
		if len(runes) == 2 {
			return tens * 10
		}
		if len(runes) == 3 {
			return tens*10 + digits[runes[2]]
		}
	}
	return 0
}

func normalizeTitle(s string) string {
	s = strings.ToLower(s)
	s = stripAll(s, matchBracketsRe)
	s = replacePunctWithSpace(s)
	return strings.Join(strings.Fields(s), " ")
}

func stripAll(s string, re *regexp.Regexp) string {
	for {
		next := re.ReplaceAllString(s, " ")
		if next == s {
			return next
		}
		s = next
	}
}

func replacePunctWithSpace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func diceBigram(a, b string) float64 {
	ra := []rune(a)
	rb := []rune(b)
	if len(ra) < 2 || len(rb) < 2 {
		return 0
	}
	ba := countBigrams(ra)
	bb := countBigrams(rb)
	inter := 0
	for g, ca := range ba {
		if cb, ok := bb[g]; ok {
			if ca < cb {
				inter += ca
			} else {
				inter += cb
			}
		}
	}
	den := float64(len(ra) - 1 + len(rb) - 1)
	if den == 0 {
		return 0
	}
	return 2 * float64(inter) / den
}

func countBigrams(rs []rune) map[string]int {
	counts := make(map[string]int, len(rs)-1)
	for i := 0; i < len(rs)-1; i++ {
		counts[string(rs[i:i+2])]++
	}
	return counts
}
