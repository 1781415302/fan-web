package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fan-web/services/matching"
)

const (
	matchingSearchLimit           = 20
	matchingLegacyBackfill        = 5
	matchingMaxHTTPAttempts       = 2000
	matchingSessionMaxDuration    = 10 * time.Minute
	matchingRequestTimeout        = 10 * time.Second
	matchingMinRequestInterval    = 200 * time.Millisecond
	matchingRetryBackoff1         = 400 * time.Millisecond
	matchingRetryBackoff2         = 800 * time.Millisecond
	matchingRetryAfterCap         = 5 * time.Second
	matchingMaxAttemptsPerRequest = 3
)

// matchingSearchResult Provider.Search 输出；Saturated 按原始成功响应是否达端点 limit。
type matchingSearchResult struct {
	Subjects  []matching.Subject
	Saturated bool
	Source    string // "v0" / "legacy" / "combined"
}

// matchingProvider 包内端口：协议适配、预算、缓存、重试、降级。
type matchingProvider interface {
	Search(context.Context, string) (matchingSearchResult, error)
	Detail(context.Context, int) (matching.Subject, error)
	Chapters(context.Context, int) ([]matching.Chapter, error)
}

// matchingClock 可注入，测试不真实等待。
type matchingClock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

type realMatchingClock struct{}

func (realMatchingClock) Now() time.Time        { return time.Now() }
func (realMatchingClock) Sleep(d time.Duration) { time.Sleep(d) }

type bangumiMatchingProvider struct {
	bangumi *BangumiService
	clock   matchingClock

	started     time.Time
	deadline    time.Time
	attempts    int
	lastRequest time.Time

	v0Disabled   bool
	v0LogicFails int

	searchOK   map[string]matchingSearchResult
	searchErr  map[string]error
	detailOK   map[int]matching.Subject
	detailErr  map[int]error
	chapterOK  map[int][]matching.Chapter
	chapterErr map[int]error
}

func newBangumiMatchingProvider(b *BangumiService, clock matchingClock) *bangumiMatchingProvider {
	if clock == nil {
		clock = realMatchingClock{}
	}
	now := clock.Now()
	return &bangumiMatchingProvider{
		bangumi:    b,
		clock:      clock,
		started:    now,
		deadline:   now.Add(matchingSessionMaxDuration),
		searchOK:   make(map[string]matchingSearchResult),
		searchErr:  make(map[string]error),
		detailOK:   make(map[int]matching.Subject),
		detailErr:  make(map[int]error),
		chapterOK:  make(map[int][]matching.Chapter),
		chapterErr: make(map[int]error),
	}
}

func (p *bangumiMatchingProvider) Search(ctx context.Context, keyword string) (matchingSearchResult, error) {
	kw := strings.TrimSpace(keyword)
	if kw == "" {
		return matchingSearchResult{Subjects: []matching.Subject{}, Source: "v0"}, nil
	}
	if cached, ok := p.searchOK[kw]; ok {
		return cloneSearchResult(cached), nil
	}
	if err, ok := p.searchErr[kw]; ok {
		return matchingSearchResult{}, err
	}

	var (
		result matchingSearchResult
		err    error
	)
	if !p.v0Disabled {
		result, err = p.searchV0(ctx, kw)
		if err == nil {
			p.searchOK[kw] = cloneSearchResult(result)
			return cloneSearchResult(result), nil
		}
		if isV0ProtocolFailure(err) {
			p.v0Disabled = true
			p.v0LogicFails++
			// fall through to legacy
		} else {
			p.rememberSearchErr(kw, err)
			return matchingSearchResult{}, err
		}
	}

	result, err = p.searchLegacy(ctx, kw)
	if err != nil {
		p.rememberSearchErr(kw, err)
		return matchingSearchResult{}, err
	}
	p.searchOK[kw] = cloneSearchResult(result)
	return cloneSearchResult(result), nil
}

func (p *bangumiMatchingProvider) rememberSearchErr(kw string, err error) {
	if _, ok := p.searchOK[kw]; ok {
		return // 成功缓存不得被错误覆写
	}
	p.searchErr[kw] = err
}

func (p *bangumiMatchingProvider) Detail(ctx context.Context, id int) (matching.Subject, error) {
	if id <= 0 {
		return matching.Subject{}, fmt.Errorf("条目 ID 无效: %w", ErrMatchUpstream)
	}
	if sub, ok := p.detailOK[id]; ok {
		return cloneSubject(sub), nil
	}
	if err, ok := p.detailErr[id]; ok {
		return matching.Subject{}, err
	}

	path := fmt.Sprintf("/v0/subjects/%d", id)
	raw, status, hdr, err := p.doHTTPWithRetry(ctx, http.MethodGet, path, nil)
	_ = hdr
	if err != nil {
		p.rememberDetailErr(id, err)
		return matching.Subject{}, err
	}
	if status == http.StatusNotFound {
		err = fmt.Errorf("条目不存在: %w", ErrBangumiNotFound)
		p.rememberDetailErr(id, err)
		return matching.Subject{}, err
	}
	if status != http.StatusOK {
		err = fmt.Errorf("详情状态码 %d: %w", status, ErrMatchUpstream)
		p.rememberDetailErr(id, err)
		return matching.Subject{}, err
	}

	sub, err := parseMatchingSubjectDetail(raw, id)
	if err != nil {
		p.rememberDetailErr(id, err)
		return matching.Subject{}, err
	}
	p.detailOK[id] = cloneSubject(sub)
	return cloneSubject(sub), nil
}

func (p *bangumiMatchingProvider) rememberDetailErr(id int, err error) {
	if _, ok := p.detailOK[id]; ok {
		return
	}
	p.detailErr[id] = err
}

func (p *bangumiMatchingProvider) Chapters(ctx context.Context, id int) ([]matching.Chapter, error) {
	if id <= 0 {
		return nil, fmt.Errorf("条目 ID 无效: %w", ErrMatchUpstream)
	}
	if ch, ok := p.chapterOK[id]; ok {
		return cloneChapters(ch), nil
	}
	if err, ok := p.chapterErr[id]; ok {
		return nil, err
	}

	all := make([]matching.Chapter, 0)
	offset := 0
	for page := 0; page < maxBangumiPages; page++ {
		path := fmt.Sprintf("/v0/episodes?subject_id=%d&type=0&limit=%d&offset=%d",
			id, subjectEpisodePageLimit, offset)
		raw, status, _, err := p.doHTTPWithRetry(ctx, http.MethodGet, path, nil)
		if err != nil {
			p.rememberChapterErr(id, err)
			return nil, err
		}
		if status == http.StatusNotFound {
			// NSFW 无 PAT 可能 404；按既有公开章节语义返回错误给调用方
			err = fmt.Errorf("章节不存在: %w", ErrBangumiNotFound)
			p.rememberChapterErr(id, err)
			return nil, err
		}
		if status != http.StatusOK {
			err = fmt.Errorf("章节状态码 %d: %w", status, ErrMatchUpstream)
			p.rememberChapterErr(id, err)
			return nil, err
		}
		var pageBody struct {
			Data []struct {
				ID   int     `json:"id"`
				Ep   float64 `json:"ep"`
				Sort float64 `json:"sort"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &pageBody); err != nil {
			err = fmt.Errorf("解析章节响应失败: %w", ErrMatchUpstream)
			p.rememberChapterErr(id, err)
			return nil, err
		}
		for _, row := range pageBody.Data {
			if row.ID <= 0 {
				err = fmt.Errorf("章节 ID 无效: %w", ErrMatchUpstream)
				p.rememberChapterErr(id, err)
				return nil, err
			}
			all = append(all, matching.Chapter{ID: row.ID, Ep: row.Ep, Sort: row.Sort})
		}
		if len(pageBody.Data) < subjectEpisodePageLimit {
			break
		}
		offset += len(pageBody.Data)
	}
	p.chapterOK[id] = cloneChapters(all)
	return cloneChapters(all), nil
}

func (p *bangumiMatchingProvider) rememberChapterErr(id int, err error) {
	if _, ok := p.chapterOK[id]; ok {
		return
	}
	p.chapterErr[id] = err
}

func (p *bangumiMatchingProvider) searchV0(ctx context.Context, keyword string) (matchingSearchResult, error) {
	body := map[string]interface{}{
		"keyword": keyword,
		"filter":  map[string]interface{}{"type": []int{2}},
	}
	path := fmt.Sprintf("/v0/search/subjects?limit=%d&offset=0", matchingSearchLimit)
	raw, status, _, err := p.doHTTPWithRetry(ctx, http.MethodPost, path, body)
	if err != nil {
		// 重试耗尽时若最终状态仍是协议不可用码，按降级处理而非硬失败。
		switch status {
		case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusGone, http.StatusNotImplemented:
			return matchingSearchResult{}, &v0ProtocolError{status: status}
		}
		return matchingSearchResult{}, err
	}
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusGone, http.StatusNotImplemented:
		return matchingSearchResult{}, &v0ProtocolError{status: status}
	case http.StatusOK:
		// ok
	default:
		if status >= 500 {
			return matchingSearchResult{}, fmt.Errorf("v0 搜索状态码 %d: %w", status, ErrMatchUpstream)
		}
		return matchingSearchResult{}, &v0ProtocolError{status: status}
	}

	var parsed struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return matchingSearchResult{}, &v0ProtocolError{cause: err, reason: "json"}
	}
	if len(bytes.TrimSpace(parsed.Data)) == 0 || bytes.Equal(bytes.TrimSpace(parsed.Data), []byte("null")) {
		return matchingSearchResult{}, &v0ProtocolError{reason: "missing data"}
	}
	var items []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &items); err != nil {
		return matchingSearchResult{}, &v0ProtocolError{cause: err, reason: "data type"}
	}

	subjects := make([]matching.Subject, 0, len(items))
	for _, item := range items {
		sub, err := parseMatchingSubjectSearch(item, false)
		if err != nil {
			return matchingSearchResult{}, fmt.Errorf("v0 搜索坏响应: %w", err)
		}
		subjects = append(subjects, sub)
	}
	saturated := len(items) >= matchingSearchLimit
	return matchingSearchResult{
		Subjects:  subjects,
		Saturated: saturated,
		Source:    "v0",
	}, nil
}

func (p *bangumiMatchingProvider) searchLegacy(ctx context.Context, keyword string) (matchingSearchResult, error) {
	path := fmt.Sprintf("/search/subject/%s?type=2&responseGroup=small&max_results=%d",
		url.PathEscape(keyword), matchingSearchLimit)
	raw, status, _, err := p.doHTTPWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return matchingSearchResult{}, err
	}
	if status == http.StatusNotFound {
		// legacy 404 常表示无结果
		return matchingSearchResult{Subjects: []matching.Subject{}, Source: "legacy"}, nil
	}
	if status != http.StatusOK {
		return matchingSearchResult{}, fmt.Errorf("legacy 搜索状态码 %d: %w", status, ErrMatchUpstream)
	}

	var parsed bgmSearchResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return matchingSearchResult{}, fmt.Errorf("解析 legacy 搜索失败: %w", ErrMatchUpstream)
	}

	stubs := make([]matching.Subject, 0, len(parsed.List))
	for _, item := range parsed.List {
		if item.ID <= 0 {
			return matchingSearchResult{}, fmt.Errorf("legacy 搜索候选 ID 无效: %w", ErrMatchUpstream)
		}
		eps := item.EpsCount
		if eps <= 0 {
			eps = item.Eps
		}
		stubs = append(stubs, matching.Subject{
			ID:           item.ID,
			Name:         item.Name,
			NameCn:       item.NameCn,
			Summary:      item.Summary,
			Cover:        toHTTPS(pickCover(item.Images)),
			EpisodeCount: eps,
			Detailed:     false,
		})
	}
	saturated := len(parsed.List) >= matchingSearchLimit

	// 按返回顺序补全前 5 个合法 ID；失败不把 ID0 混入池；Session 缓存去重由 Detail 承担。
	out := make([]matching.Subject, 0, len(stubs))
	for i, stub := range stubs {
		if i < matchingLegacyBackfill {
			detailed, err := p.Detail(ctx, stub.ID)
			if err != nil {
				continue // 跳过该 ID，不混入坏条目
			}
			if detailed.ID != stub.ID {
				continue
			}
			out = append(out, detailed)
			continue
		}
		out = append(out, stub)
	}
	return matchingSearchResult{
		Subjects:  out,
		Saturated: saturated, // 补全/合并后仍保留原始饱和标记
		Source:    "legacy",
	}, nil
}

type v0ProtocolError struct {
	status int
	reason string
	cause  error
}

func (e *v0ProtocolError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("v0 协议失败 status=%d reason=%s: %v", e.status, e.reason, e.cause)
	}
	return fmt.Sprintf("v0 协议失败 status=%d reason=%s", e.status, e.reason)
}

func (e *v0ProtocolError) Unwrap() error { return ErrMatchUpstream }

func isV0ProtocolFailure(err error) bool {
	var pe *v0ProtocolError
	return errors.As(err, &pe)
}

func (p *bangumiMatchingProvider) doHTTPWithRetry(ctx context.Context, method, path string, body any) ([]byte, int, http.Header, error) {
	var lastErr error
	var lastStatus int
	var lastHdr http.Header
	var lastBody []byte

	for attempt := 0; attempt < matchingMaxAttemptsPerRequest; attempt++ {
		if attempt > 0 {
			wait := matchingRetryBackoff1
			if attempt >= 2 {
				wait = matchingRetryBackoff2
			}
			if lastStatus == http.StatusTooManyRequests {
				if ra := parseRetryAfter(lastHdr); ra > 0 {
					wait = ra
				}
			}
			p.clock.Sleep(wait)
		}

		raw, status, hdr, err := p.doHTTPOnce(ctx, method, path, body)
		lastStatus, lastHdr, lastBody = status, hdr, raw
		if err != nil {
			lastErr = err
			// 网络错误可重试
			continue
		}
		if status == http.StatusTooManyRequests || status == http.StatusInternalServerError ||
			status == http.StatusBadGateway || status == http.StatusServiceUnavailable ||
			status == http.StatusGatewayTimeout {
			lastErr = fmt.Errorf("上游状态码 %d: %w", status, ErrMatchUpstream)
			continue
		}
		return raw, status, hdr, nil
	}
	if lastErr != nil {
		return lastBody, lastStatus, lastHdr, lastErr
	}
	return lastBody, lastStatus, lastHdr, fmt.Errorf("上游重试耗尽 status=%d: %w", lastStatus, ErrMatchUpstream)
}

func parseRetryAfter(hdr http.Header) time.Duration {
	if hdr == nil {
		return 0
	}
	raw := strings.TrimSpace(hdr.Get("Retry-After"))
	if raw == "" {
		return 0
	}
	sec, err := strconv.Atoi(raw)
	if err != nil || sec <= 0 {
		return 0
	}
	d := time.Duration(sec) * time.Second
	if d > matchingRetryAfterCap {
		return matchingRetryAfterCap
	}
	return d
}

func (p *bangumiMatchingProvider) doHTTPOnce(ctx context.Context, method, path string, body any) ([]byte, int, http.Header, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, nil, err
	}
	now := p.clock.Now()
	if now.After(p.deadline) {
		return nil, 0, nil, fmt.Errorf("匹配会话超时: %w", ErrMatchUpstream)
	}
	if p.attempts >= matchingMaxHTTPAttempts {
		return nil, 0, nil, fmt.Errorf("匹配请求预算耗尽: %w", ErrMatchUpstream)
	}
	if !p.lastRequest.IsZero() {
		elapsed := now.Sub(p.lastRequest)
		if elapsed < matchingMinRequestInterval {
			p.clock.Sleep(matchingMinRequestInterval - elapsed)
		}
	}

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, nil, err
		}
		reader = bytes.NewReader(raw)
	}
	reqCtx, cancel := context.WithTimeout(ctx, matchingRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, method, p.bangumi.endpoint(path), reader)
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("User-Agent", bangumiUA)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// 新公共请求不带用户 PAT：故意不设置 Authorization。

	p.attempts++
	p.lastRequest = p.clock.Now()
	resp, err := p.bangumi.client.Do(req)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("请求 Bangumi 失败: %w", err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, maxBangumiResponseBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, resp.StatusCode, resp.Header.Clone(), fmt.Errorf("读取 Bangumi 响应失败: %w", err)
	}
	if len(raw) > maxBangumiResponseBytes {
		return nil, resp.StatusCode, resp.Header.Clone(), fmt.Errorf("Bangumi 响应过大: %w", ErrMatchUpstream)
	}
	return raw, resp.StatusCode, resp.Header.Clone(), nil
}

func parseMatchingSubjectSearch(raw json.RawMessage, detailed bool) (matching.Subject, error) {
	var item struct {
		ID            int             `json:"id"`
		Name          string          `json:"name"`
		NameCn        string          `json:"name_cn"`
		Summary       string          `json:"summary"`
		Date          string          `json:"date"`
		Platform      string          `json:"platform"`
		Eps           int             `json:"eps"`
		TotalEpisodes int             `json:"total_episodes"`
		Images        bgmImages       `json:"images"`
		Infobox       json.RawMessage `json:"infobox"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return matching.Subject{}, fmt.Errorf("解析候选失败: %w", ErrMatchUpstream)
	}
	if item.ID <= 0 {
		return matching.Subject{}, fmt.Errorf("搜索候选 ID≤0: %w", ErrMatchUpstream)
	}
	eps := item.TotalEpisodes
	if eps <= 0 {
		eps = item.Eps
	}
	return matching.Subject{
		ID:           item.ID,
		Name:         item.Name,
		NameCn:       item.NameCn,
		Aliases:      parseInfoboxAliases(item.Infobox),
		Year:         matchingYearFromDate(item.Date),
		Media:        matchingMediaFromPlatform(item.Platform),
		EpisodeCount: eps,
		Summary:      item.Summary,
		Cover:        toHTTPS(pickCover(item.Images)),
		Platform:     item.Platform,
		Detailed:     detailed,
	}, nil
}

func parseMatchingSubjectDetail(raw []byte, wantID int) (matching.Subject, error) {
	var item struct {
		ID            int             `json:"id"`
		Name          string          `json:"name"`
		NameCn        string          `json:"name_cn"`
		Summary       string          `json:"summary"`
		Date          string          `json:"date"`
		Platform      string          `json:"platform"`
		Eps           int             `json:"eps"`
		TotalEpisodes int             `json:"total_episodes"`
		Images        bgmImages       `json:"images"`
		Infobox       json.RawMessage `json:"infobox"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return matching.Subject{}, fmt.Errorf("解析详情失败: %w", ErrMatchUpstream)
	}
	if item.ID <= 0 || item.ID != wantID {
		return matching.Subject{}, fmt.Errorf("详情 ID 不匹配 want=%d got=%d: %w", wantID, item.ID, ErrMatchUpstream)
	}
	name := strings.TrimSpace(item.Name)
	nameCn := strings.TrimSpace(item.NameCn)
	if name == "" && nameCn == "" {
		return matching.Subject{}, fmt.Errorf("详情名称为空: %w", ErrMatchUpstream)
	}
	eps := item.TotalEpisodes
	if eps <= 0 {
		eps = item.Eps
	}
	return matching.Subject{
		ID:           item.ID,
		Name:         item.Name,
		NameCn:       item.NameCn,
		Aliases:      parseInfoboxAliases(item.Infobox),
		Year:         matchingYearFromDate(item.Date),
		Media:        matchingMediaFromPlatform(item.Platform),
		EpisodeCount: eps,
		Summary:      item.Summary,
		Cover:        toHTTPS(pickCover(item.Images)),
		Platform:     item.Platform,
		Detailed:     true,
	}, nil
}

func matchingYearFromDate(date string) int {
	date = strings.TrimSpace(date)
	if len(date) < 4 {
		return 0
	}
	y, err := strconv.Atoi(date[:4])
	if err != nil || y < 1900 || y > 2099 {
		return 0
	}
	return y
}

func matchingMediaFromPlatform(platform string) matching.MediaKind {
	p := strings.TrimSpace(platform)
	if p == "" {
		return matching.KindUnknown
	}
	lower := strings.ToLower(p)
	switch {
	case strings.Contains(p, "剧场版"), strings.Contains(p, "电影"), strings.Contains(lower, "movie"):
		return matching.KindMovie
	case strings.Contains(lower, "ova"), strings.Contains(lower, "oad"):
		return matching.KindOVA
	case strings.Contains(lower, "tv"), strings.Contains(lower, "web"):
		return matching.KindSeries
	default:
		return matching.KindUnknown
	}
}

func cloneSearchResult(in matchingSearchResult) matchingSearchResult {
	out := matchingSearchResult{
		Saturated: in.Saturated,
		Source:    in.Source,
		Subjects:  make([]matching.Subject, len(in.Subjects)),
	}
	for i := range in.Subjects {
		out.Subjects[i] = cloneSubject(in.Subjects[i])
	}
	return out
}

func cloneSubject(s matching.Subject) matching.Subject {
	out := s
	if s.Aliases != nil {
		out.Aliases = append([]string(nil), s.Aliases...)
	}
	return out
}

func cloneChapters(in []matching.Chapter) []matching.Chapter {
	if in == nil {
		return []matching.Chapter{}
	}
	return append([]matching.Chapter(nil), in...)
}
