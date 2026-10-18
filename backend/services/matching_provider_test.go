package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"fan-web/services/matching"
)

type fakeMatchingClock struct {
	mu    sync.Mutex
	now   time.Time
	slept []time.Duration
}

func newFakeMatchingClock(start time.Time) *fakeMatchingClock {
	return &fakeMatchingClock{now: start}
}

func (c *fakeMatchingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeMatchingClock) Sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.slept = append(c.slept, d)
	c.now = c.now.Add(d)
}

func (c *fakeMatchingClock) sleptTotal() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	var sum time.Duration
	for _, d := range c.slept {
		sum += d
	}
	return sum
}

func newTestBangumi(serverURL string) *BangumiService {
	b := NewBangumiService()
	b.SetBaseURL(serverURL)
	b.client = &http.Client{Timeout: 5 * time.Second}
	return b
}

func TestMatchingProviderSearchV0PathUANoPAT(t *testing.T) {
	var sawAuth bool
	var gotUA, gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotUA = r.Header.Get("User-Agent")
		if r.Header.Get("Authorization") != "" {
			sawAuth = true
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"id": 101, "name": "Show A", "name_cn": "番A", "date": "2020-01-02", "platform": "TV"},
			},
		})
	}))
	defer srv.Close()

	clock := newFakeMatchingClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	p := newBangumiMatchingProvider(newTestBangumi(srv.URL), clock)
	res, err := p.Search(context.Background(), "Show A")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/v0/search/subjects" {
		t.Fatalf("method/path=%s %s", gotMethod, gotPath)
	}
	if gotUA != bangumiUA {
		t.Fatalf("UA=%q want %q", gotUA, bangumiUA)
	}
	if sawAuth {
		t.Fatal("public search must not send Authorization/PAT")
	}
	if res.Source != "v0" || len(res.Subjects) != 1 || res.Subjects[0].ID != 101 {
		t.Fatalf("result=%+v", res)
	}
	if res.Subjects[0].Detailed {
		t.Fatal("search candidates must be Detailed=false")
	}
	if res.Subjects[0].Year != 2020 || res.Subjects[0].Media != matching.KindSeries {
		t.Fatalf("year/media=%d %q", res.Subjects[0].Year, res.Subjects[0].Media)
	}
	if res.Saturated {
		t.Fatal("single hit must not be saturated")
	}
}

func TestMatchingProviderSearchSaturatedFromRawLimit(t *testing.T) {
	items := make([]map[string]interface{}, matchingSearchLimit)
	for i := range items {
		items[i] = map[string]interface{}{"id": i + 1, "name": "X"}
	}
	// Also include a duplicate-looking pool after "dedupe" would shrink — Saturated must stay true.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": items})
	}))
	defer srv.Close()
	p := newBangumiMatchingProvider(newTestBangumi(srv.URL), newFakeMatchingClock(time.Now()))
	res, err := p.Search(context.Background(), "popular")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !res.Saturated {
		t.Fatal("raw len==limit must set Saturated; must not infer from deduped pool size")
	}
}

func TestMatchingProviderSearchRejectsNonPositiveID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{{"id": 0, "name": "bad"}},
		})
	}))
	defer srv.Close()
	p := newBangumiMatchingProvider(newTestBangumi(srv.URL), newFakeMatchingClock(time.Now()))
	_, err := p.Search(context.Background(), "bad")
	if err == nil {
		t.Fatal("want error for ID≤0")
	}
}

func TestMatchingProviderSearchDegradesToLegacyOnV0ProtocolFailure(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case strings.HasPrefix(r.URL.Path, "/v0/search/subjects"):
			w.WriteHeader(http.StatusNotFound)
		case strings.HasPrefix(r.URL.Path, "/search/subject/"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"list": []map[string]interface{}{
					{"id": 7, "name": "Legacy Hit", "name_cn": "旧命中", "eps": 12},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/v0/subjects/"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": 7, "name": "Legacy Hit", "name_cn": "旧命中", "platform": "TV", "total_episodes": 12,
				"infobox": []map[string]interface{}{{"key": "别名", "value": "Alias Seven"}},
			})
		default:
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	defer srv.Close()

	p := newBangumiMatchingProvider(newTestBangumi(srv.URL), newFakeMatchingClock(time.Now()))
	res, err := p.Search(context.Background(), "legacy-kw")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Source != "legacy" {
		t.Fatalf("source=%q", res.Source)
	}
	if len(res.Subjects) != 1 || !res.Subjects[0].Detailed || res.Subjects[0].ID != 7 {
		t.Fatalf("subjects=%+v", res.Subjects)
	}
	if !p.v0Disabled {
		t.Fatal("v0 should be disabled after protocol failure")
	}
	// Second search should skip v0 entirely.
	paths = nil
	_, err = p.Search(context.Background(), "another")
	if err != nil {
		t.Fatalf("second Search: %v", err)
	}
	for _, path := range paths {
		if strings.Contains(path, "/v0/search/") {
			t.Fatalf("v0 used after disable: %v", paths)
		}
	}
}

func TestMatchingProviderSearchDegradesOnMissingData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v0/search/") {
			_, _ = w.Write([]byte(`{"total":0}`)) // missing data
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"list": []interface{}{}})
	}))
	defer srv.Close()
	p := newBangumiMatchingProvider(newTestBangumi(srv.URL), newFakeMatchingClock(time.Now()))
	res, err := p.Search(context.Background(), "x")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Source != "legacy" || !p.v0Disabled {
		t.Fatalf("source=%q disabled=%v", res.Source, p.v0Disabled)
	}
}

func TestMatchingProviderLegacyBackfillSkipsFailedDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v0/search/"):
			w.WriteHeader(http.StatusNotImplemented)
		case strings.HasPrefix(r.URL.Path, "/search/subject/"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"list": []map[string]interface{}{
					{"id": 1, "name": "A"},
					{"id": 2, "name": "B"},
				},
			})
		case r.URL.Path == "/v0/subjects/1":
			w.WriteHeader(http.StatusInternalServerError)
		case r.URL.Path == "/v0/subjects/2":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 2, "name": "B", "platform": "WEB"})
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	clock := newFakeMatchingClock(time.Now())
	p := newBangumiMatchingProvider(newTestBangumi(srv.URL), clock)
	res, err := p.Search(context.Background(), "pair")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Subjects) != 1 || res.Subjects[0].ID != 2 {
		t.Fatalf("want only successful backfill id=2, got %+v", res.Subjects)
	}
	for _, s := range res.Subjects {
		if s.ID == 0 {
			t.Fatal("must not mix ID 0 into pool")
		}
	}
}

func TestMatchingProviderDetailValidatesIDAndName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/subjects/5":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 9, "name": "Wrong"})
		case "/v0/subjects/6":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 6, "name": "", "name_cn": ""})
		case "/v0/subjects/8":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": 8, "name": "Ok", "date": "2019-05-01", "platform": "剧场版", "total_episodes": 1,
			})
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	p := newBangumiMatchingProvider(newTestBangumi(srv.URL), newFakeMatchingClock(time.Now()))
	if _, err := p.Detail(context.Background(), 0); err == nil {
		t.Fatal("id 0 must error")
	}
	if _, err := p.Detail(context.Background(), 5); err == nil {
		t.Fatal("mismatched id must error")
	}
	if _, err := p.Detail(context.Background(), 6); err == nil {
		t.Fatal("empty name must error")
	}
	sub, err := p.Detail(context.Background(), 8)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if !sub.Detailed || sub.Year != 2019 || sub.Media != matching.KindMovie {
		t.Fatalf("subject=%+v", sub)
	}
}

func TestMatchingProviderDetailCachesSuccessNotErrorAsEmpty(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	clock := newFakeMatchingClock(time.Now())
	p := newBangumiMatchingProvider(newTestBangumi(srv.URL), clock)
	_, err := p.Detail(context.Background(), 3)
	if err == nil {
		t.Fatal("first detail should fail after retries")
	}
	// Error remembered — no empty success cache.
	if _, ok := p.detailOK[3]; ok {
		t.Fatal("error must not be cached as empty success")
	}
	hitsBefore := hits
	_, err2 := p.Detail(context.Background(), 3)
	if err2 == nil {
		t.Fatal("cached error should return without turning into empty subject")
	}
	if hits != hitsBefore {
		t.Fatalf("cached error must not re-hit HTTP, hits %d→%d", hitsBefore, hits)
	}
	if hits < matchingMaxAttemptsPerRequest {
		t.Fatalf("hits=%d", hits)
	}
}

func TestMatchingProviderSearchCachesSuccessAndSkipsHTTP(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{{"id": 42, "name": "Cached"}},
		})
	}))
	defer srv.Close()
	p := newBangumiMatchingProvider(newTestBangumi(srv.URL), newFakeMatchingClock(time.Now()))
	a, err := p.Search(context.Background(), "cached")
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.Search(context.Background(), "cached")
	if err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("cache should skip HTTP, hits=%d", hits)
	}
	if a.Subjects[0].ID != b.Subjects[0].ID {
		t.Fatal("cached mismatch")
	}
}

func TestMatchingProviderRetryBackoffAndRetryAfter(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.Header().Set("Retry-After", "9") // cap to 5s
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 11, "name": "Ok"})
	}))
	defer srv.Close()
	clock := newFakeMatchingClock(time.Now())
	p := newBangumiMatchingProvider(newTestBangumi(srv.URL), clock)
	sub, err := p.Detail(context.Background(), 11)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if sub.ID != 11 {
		t.Fatalf("%+v", sub)
	}
	if hits != 3 {
		t.Fatalf("hits=%d want 3 attempts", hits)
	}
	// Two sleeps between 3 attempts; Retry-After capped at 5s.
	if len(clock.slept) < 2 {
		t.Fatalf("slept=%v", clock.slept)
	}
	for _, d := range clock.slept {
		if d > matchingRetryAfterCap && d != matchingMinRequestInterval {
			// min interval sleeps also recorded; Retry-After sleeps must be <= cap
			if d == matchingRetryAfterCap || d == matchingRetryBackoff1 || d == matchingRetryBackoff2 || d == matchingMinRequestInterval {
				continue
			}
			if d > matchingRetryAfterCap {
				t.Fatalf("Retry-After not capped: %v", d)
			}
		}
	}
	foundCap := false
	for _, d := range clock.slept {
		if d == matchingRetryAfterCap {
			foundCap = true
		}
	}
	if !foundCap {
		t.Fatalf("expected capped Retry-After sleep, slept=%v", clock.slept)
	}
}

func TestMatchingProviderBudgetExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 1, "name": "n"})
	}))
	defer srv.Close()
	clock := newFakeMatchingClock(time.Now())
	p := newBangumiMatchingProvider(newTestBangumi(srv.URL), clock)
	p.attempts = matchingMaxHTTPAttempts
	_, err := p.Detail(context.Background(), 1)
	if err == nil || !errors.Is(err, ErrMatchUpstream) {
		t.Fatalf("want ErrMatchUpstream, got %v", err)
	}
}

func TestMatchingProviderSessionDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 1, "name": "n"})
	}))
	defer srv.Close()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := newFakeMatchingClock(start.Add(matchingSessionMaxDuration + time.Second))
	p := newBangumiMatchingProvider(newTestBangumi(srv.URL), clock)
	// Fix started/deadline relative to original construction — reconstruct with past deadline.
	p.started = start
	p.deadline = start.Add(matchingSessionMaxDuration)
	_, err := p.Detail(context.Background(), 1)
	if err == nil || !errors.Is(err, ErrMatchUpstream) {
		t.Fatalf("want deadline ErrMatchUpstream, got %v", err)
	}
}

func TestMatchingProviderChaptersNoPATAndPagination(t *testing.T) {
	var sawAuth bool
	var pages int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			sawAuth = true
		}
		pages++
		offset := r.URL.Query().Get("offset")
		if offset == "0" {
			data := make([]map[string]interface{}, subjectEpisodePageLimit)
			for i := range data {
				data[i] = map[string]interface{}{"id": i + 1, "ep": float64(i + 1), "sort": float64(i + 1)}
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{{"id": 999, "ep": 999, "sort": 999}},
		})
	}))
	defer srv.Close()
	p := newBangumiMatchingProvider(newTestBangumi(srv.URL), newFakeMatchingClock(time.Now()))
	chs, err := p.Chapters(context.Background(), 55)
	if err != nil {
		t.Fatalf("Chapters: %v", err)
	}
	if sawAuth {
		t.Fatal("chapters must not send PAT")
	}
	if pages != 2 || len(chs) != subjectEpisodePageLimit+1 {
		t.Fatalf("pages=%d len=%d", pages, len(chs))
	}
	// cache
	pages = 0
	chs2, err := p.Chapters(context.Background(), 55)
	if err != nil || pages != 0 || len(chs2) != len(chs) {
		t.Fatalf("cache broken pages=%d err=%v", pages, err)
	}
}

func TestMatchingProviderResponseSizeLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"name":"`))
		_, _ = io.Copy(w, strings.NewReader(strings.Repeat("x", maxBangumiResponseBytes+100)))
		_, _ = w.Write([]byte(`"}`))
	}))
	defer srv.Close()
	p := newBangumiMatchingProvider(newTestBangumi(srv.URL), newFakeMatchingClock(time.Now()))
	_, err := p.Detail(context.Background(), 1)
	if err == nil || !errors.Is(err, ErrMatchUpstream) {
		t.Fatalf("want size ErrMatchUpstream, got %v", err)
	}
}

func TestMatchingProviderLegacySaturatedPreservedAfterBackfill(t *testing.T) {
	list := make([]map[string]interface{}, matchingSearchLimit)
	for i := range list {
		list[i] = map[string]interface{}{"id": i + 1, "name": "N"}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v0/search/"):
			w.WriteHeader(http.StatusGone)
		case strings.HasPrefix(r.URL.Path, "/search/subject/"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"list": list})
		case strings.HasPrefix(r.URL.Path, "/v0/subjects/"):
			parts := strings.Split(r.URL.Path, "/")
			id := 0
			for _, c := range parts[len(parts)-1] {
				id = id*10 + int(c-'0')
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "name": "N"})
		}
	}))
	defer srv.Close()
	p := newBangumiMatchingProvider(newTestBangumi(srv.URL), newFakeMatchingClock(time.Now()))
	res, err := p.Search(context.Background(), "sat")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !res.Saturated {
		t.Fatal("Saturated must remain true after backfill/merge")
	}
	if len(res.Subjects) < matchingLegacyBackfill {
		t.Fatalf("len=%d", len(res.Subjects))
	}
}

func TestMatchingProviderInjectedIntoSessionHelper(t *testing.T) {
	fake := matchingProviderFunc{
		search: func(ctx context.Context, q string) (matchingSearchResult, error) {
			return matchingSearchResult{Subjects: []matching.Subject{{ID: 1, Name: q}}, Source: "v0"}, nil
		},
	}
	rt, err := NewMatchingRuntime(nil, matching.EngineV2, "")
	if err != nil {
		t.Fatal(err)
	}
	sess := newMatchingSessionForTest(context.Background(), rt, fake)
	res, err := sess.provider.Search(context.Background(), "x")
	if err != nil || res.Subjects[0].ID != 1 {
		t.Fatalf("%v %+v", err, res)
	}
}

type matchingProviderFunc struct {
	search   func(context.Context, string) (matchingSearchResult, error)
	detail   func(context.Context, int) (matching.Subject, error)
	chapters func(context.Context, int) ([]matching.Chapter, error)
}

func (f matchingProviderFunc) Search(ctx context.Context, q string) (matchingSearchResult, error) {
	if f.search != nil {
		return f.search(ctx, q)
	}
	return matchingSearchResult{}, errors.New("no search")
}
func (f matchingProviderFunc) Detail(ctx context.Context, id int) (matching.Subject, error) {
	if f.detail != nil {
		return f.detail(ctx, id)
	}
	return matching.Subject{}, errors.New("no detail")
}
func (f matchingProviderFunc) Chapters(ctx context.Context, id int) ([]matching.Chapter, error) {
	if f.chapters != nil {
		return f.chapters(ctx, id)
	}
	return nil, errors.New("no chapters")
}
