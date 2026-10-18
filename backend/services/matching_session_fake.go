package services

import (
	"context"
	"fmt"
	"strings"

	"fan-web/services/matching"
)

// fakeSessionProvider 包内/跨包测试用：不联网。
type fakeSessionProvider struct {
	search   map[string]matchingSearchResult
	detail   map[int]matching.Subject
	chapters map[int][]matching.Chapter

	searchCalls []string
	detailCalls []int
}

func newFakeSessionProvider() *fakeSessionProvider {
	return &fakeSessionProvider{
		search:   map[string]matchingSearchResult{},
		detail:   map[int]matching.Subject{},
		chapters: map[int][]matching.Chapter{},
	}
}

func (f *fakeSessionProvider) Search(_ context.Context, kw string) (matchingSearchResult, error) {
	f.searchCalls = append(f.searchCalls, kw)
	if res, ok := f.search[kw]; ok {
		return cloneSearchResult(res), nil
	}
	// 子串兜底：便于不同查询词命中同一夹具
	for k, res := range f.search {
		if strings.Contains(kw, k) || strings.Contains(k, kw) {
			return cloneSearchResult(res), nil
		}
	}
	return matchingSearchResult{Subjects: []matching.Subject{}, Source: "v0"}, nil
}

func (f *fakeSessionProvider) Detail(_ context.Context, id int) (matching.Subject, error) {
	f.detailCalls = append(f.detailCalls, id)
	sub, ok := f.detail[id]
	if !ok {
		return matching.Subject{}, fmt.Errorf("no detail for %d: %w", id, ErrMatchUpstream)
	}
	if sub.ID != id {
		return matching.Subject{}, fmt.Errorf("detail id mismatch: %w", ErrMatchUpstream)
	}
	out := cloneSubject(sub)
	out.Detailed = true
	return out, nil
}

func (f *fakeSessionProvider) Chapters(_ context.Context, id int) ([]matching.Chapter, error) {
	if ch, ok := f.chapters[id]; ok {
		return cloneChapters(ch), nil
	}
	return []matching.Chapter{}, nil
}

func (f *fakeSessionProvider) putSubject(sub matching.Subject, searchKeys ...string) {
	sub = cloneSubject(sub)
	f.detail[sub.ID] = sub
	stub := cloneSubject(sub)
	stub.Detailed = false
	for _, k := range searchKeys {
		res := f.search[k]
		res.Subjects = append(res.Subjects, stub)
		res.Source = "v0"
		f.search[k] = res
	}
}

// FakeMatchSeed 跨包测试夹具：一条可检索的条目与可选章节。
type FakeMatchSeed struct {
	Subject    matching.Subject
	SearchKeys []string
	Chapters   []matching.Chapter
}

// InstallFakeScanSession 将 ScanForAnime 打开的 Session 换成假 Provider；返回 restore。
// 仅测试使用，不作为生产配置开关。
func InstallFakeScanSession(seeds ...FakeMatchSeed) func() {
	return InstallFakeScanSessionWithOpenHook(nil, seeds...)
}

// InstallFakeScanSessionWithOpenHook 同 InstallFakeScanSession，并在打开 Session 时调用 onOpen
// （用于模拟扫描等待期间的并发写库，例如 CAS）。
func InstallFakeScanSessionWithOpenHook(onOpen func(), seeds ...FakeMatchSeed) func() {
	f := newFakeSessionProvider()
	for _, seed := range seeds {
		f.putSubject(seed.Subject, seed.SearchKeys...)
		if seed.Chapters != nil {
			f.chapters[seed.Subject.ID] = cloneChapters(seed.Chapters)
		}
	}
	prev := scanSessionHook
	scanSessionHook = func(ctx context.Context, rt *MatchingRuntime) *MatchingSession {
		if onOpen != nil {
			onOpen()
		}
		return newMatchingSessionForTest(ctx, rt, f)
	}
	return func() { scanSessionHook = prev }
}
