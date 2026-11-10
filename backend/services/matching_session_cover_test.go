package services

import (
	"context"
	"testing"

	"fan-web/services/matching"
)

// Plan §4.6：runQueries 语言覆盖放宽——解析层清理后，仅由低信息词（元数据残留、纯数字、
// 字幕组名）构成的标题不计入 neededLangs。以下用现场 A 的清理前/后两个版本锁定规则。

const coverReZeroTitle = "Re Zero kara Hajimeru Isekai Seikatsu"

func coverFileTitles(texts ...string) []matching.Title {
	out := make([]matching.Title, 0, len(texts))
	for _, t := range texts {
		out = append(out, matching.Title{Text: t, Source: matching.SourceFile})
	}
	return out
}

// runCoverQueries 只执行 Re Zero 一条查询词（预算即耗尽），返回 retrievalComplete。
func runCoverQueries(t *testing.T, titles []matching.Title, terms ...string) bool {
	t.Helper()
	p := newFakeSessionProvider()
	p.exactOnly = true
	p.putSubject(matching.Subject{
		ID: 547888, Name: "Re:ゼロから始める異世界生活 4th season 喪失編",
		Aliases: []string{coverReZeroTitle}, Media: matching.KindSeries, EpisodeCount: 8,
	}, coverReZeroTitle)
	s := newMatchingSessionForTest(context.Background(), nil, p)
	s.ensurePool()
	s.resetActive()
	if len(terms) == 0 {
		terms = []string{coverReZeroTitle}
	}
	qt := make([]matching.QueryTerm, 0, len(terms))
	for _, term := range terms {
		qt = append(qt, matching.QueryTerm{Text: term, Source: matching.SourceFile})
	}
	q := matching.Query{Titles: titles, HasFileTitle: true, Media: matching.KindSeries}
	ok, err := s.runQueries(q, qt, matching.Catalog{})
	if err != nil {
		t.Fatalf("runQueries err: %v", err)
	}
	if len(p.searchCalls) != len(terms) {
		t.Fatalf("searchCalls=%v want %d", p.searchCalls, len(terms))
	}
	return ok
}

func TestRunQueriesCoverSiteABeforeCleanup(t *testing.T) {
	// 清理前：ParseFile 现状把字幕组「晚街与灯」、[4th - 19]、总第85、简日双语内嵌 都当标题。
	// 无标记的字幕组名词法上无法识别为低信息 → 仍需覆盖 → 召回不完整（保守）。
	titles := coverFileTitles("晚街与灯", coverReZeroTitle, "4th - 19", "总第85", "简日双语内嵌")
	if runCoverQueries(t, titles) {
		t.Fatal("清理前版本含未标记字幕组残留，必须仍为召回不完整")
	}
}

func TestRunQueriesCoverSiteAAfterCleanup(t *testing.T) {
	// 清理后：字幕组被解析层剥离；仅剩真实标题。
	if !runCoverQueries(t, coverFileTitles(coverReZeroTitle)) {
		t.Fatal("清理后版本应召回完整")
	}
	// 清理后仍残留的低信息标题（集号/总集数/元数据）不计入 neededLangs。
	residue := coverFileTitles(coverReZeroTitle, "4th - 19", "总第85", "简日双语内嵌")
	if !runCoverQueries(t, residue) {
		t.Fatal("仅低信息残留标题不应阻止召回完整")
	}
}

func TestRunQueriesCoverLowInfoVariants(t *testing.T) {
	for _, low := range []string{
		"WebRip 1080P_AVC_AAC",
		"爱恋字幕社",
		"[Lilith-Raws]",
		"S02E05v2",
		"ABCD1234",
		"05 END",
		"12",
		"繁日雙語",
		"Dual Audio ENG SUB",
		"第05话",
	} {
		if !runCoverQueries(t, coverFileTitles(coverReZeroTitle, low)) {
			t.Fatalf("低信息标题 %q 不应计入 neededLangs", low)
		}
	}
}

func TestRunQueriesCoverInformativeTitlesStillRequired(t *testing.T) {
	for _, info := range []string{
		"从零开始的异世界生活", // 真实中文标题未被查询覆盖
		"晚街与灯",       // 无标记字幕组名：不猜测，保守保留
		"Re Zero 2nd",
		"喵萌奶茶屋",
	} {
		if runCoverQueries(t, coverFileTitles(coverReZeroTitle, info)) {
			t.Fatalf("信息性标题 %q 未覆盖时必须召回不完整", info)
		}
	}
}

func TestRunQueriesCoverAllLowInfoKeepsOriginal(t *testing.T) {
	// 全部标题都是低信息时不放宽：仍要求逐条覆盖。
	titles := coverFileTitles("4th - 19", "总第85")
	if runCoverQueries(t, titles, "总第85") {
		t.Fatal("全部低信息时不应放宽为空集合")
	}
	if !runCoverQueries(t, titles, "总第85", "4th - 19") {
		t.Fatal("逐条覆盖后应完整")
	}
}
