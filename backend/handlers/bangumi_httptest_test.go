package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"fan-web/services"
	"fan-web/services/matching"
)

// bangumiFixture 用 httptest 模拟公开 Bangumi API，供 handlers 跨包测试（不引入生产 fake）。
type bangumiFixture struct {
	subjects map[int]matching.Subject
	chapters map[int][]matching.Chapter
	keys     map[string][]int // search keyword → subject IDs
	mu       sync.Mutex
	server   *httptest.Server
	bangumi  *services.BangumiService
}

type bangumiSeed struct {
	Subject    matching.Subject
	SearchKeys []string
	Chapters   []matching.Chapter
}

func startBangumiFixture(t *testing.T, seeds ...bangumiSeed) *bangumiFixture {
	t.Helper()
	f := &bangumiFixture{
		subjects: map[int]matching.Subject{},
		chapters: map[int][]matching.Chapter{},
		keys:     map[string][]int{},
	}
	for _, seed := range seeds {
		sub := seed.Subject
		f.subjects[sub.ID] = sub
		if seed.Chapters != nil {
			f.chapters[sub.ID] = append([]matching.Chapter(nil), seed.Chapters...)
		}
		for _, k := range seed.SearchKeys {
			f.keys[k] = append(f.keys[k], sub.ID)
		}
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	f.bangumi = services.NewBangumiService()
	f.bangumi.SetBaseURL(f.server.URL)
	return f
}

func (f *bangumiFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.Path
	switch {
	case r.Method == http.MethodPost && path == "/v0/search/subjects":
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Keyword string `json:"keyword"`
		}
		_ = json.Unmarshal(body, &req)
		ids := f.keys[strings.TrimSpace(req.Keyword)]
		// 子串兜底：handlers 夹具标题较稳定
		if len(ids) == 0 {
			for k, vs := range f.keys {
				if strings.Contains(req.Keyword, k) || strings.Contains(k, req.Keyword) {
					ids = append(ids, vs...)
				}
			}
		}
		data := make([]map[string]interface{}, 0, len(ids))
		seen := map[int]bool{}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			sub := f.subjects[id]
			platform := "TV"
			if sub.Media == matching.KindMovie {
				platform = "电影"
			} else if sub.Media == matching.KindOVA {
				platform = "OVA"
			}
			date := ""
			if sub.Year > 0 {
				date = strconv.Itoa(sub.Year) + "-01-01"
			}
			data = append(data, map[string]interface{}{
				"id": sub.ID, "name": sub.Name, "name_cn": sub.NameCn,
				"date": date, "platform": platform,
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v0/subjects/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(path, "/v0/subjects/"))
		sub, ok := f.subjects[id]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		platform := "TV"
		if sub.Media == matching.KindMovie {
			platform = "电影"
		} else if sub.Media == matching.KindOVA {
			platform = "OVA"
		}
		date := ""
		if sub.Year > 0 {
			date = strconv.Itoa(sub.Year) + "-01-01"
		}
		infobox := []map[string]interface{}{}
		if len(sub.Aliases) > 0 {
			aliasObjs := make([]map[string]string, 0, len(sub.Aliases))
			for _, a := range sub.Aliases {
				aliasObjs = append(aliasObjs, map[string]string{"v": a})
			}
			infobox = append(infobox, map[string]interface{}{
				"key": "别名", "value": aliasObjs,
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": sub.ID, "name": sub.Name, "name_cn": sub.NameCn,
			"summary": sub.Summary, "date": date, "platform": platform,
			"eps": sub.EpisodeCount, "total_episodes": sub.EpisodeCount,
			"images": map[string]string{}, "infobox": infobox,
		})
	case r.Method == http.MethodGet && path == "/v0/episodes":
		sid, _ := strconv.Atoi(r.URL.Query().Get("subject_id"))
		chs := f.chapters[sid]
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 {
			limit = 100
		}
		if offset > len(chs) {
			offset = len(chs)
		}
		end := offset + limit
		if end > len(chs) {
			end = len(chs)
		}
		page := chs[offset:end]
		data := make([]map[string]interface{}, 0, len(page))
		for _, ch := range page {
			data = append(data, map[string]interface{}{
				"id": ch.ID, "ep": ch.Ep, "sort": ch.Sort,
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
	default:
		// legacy search fallback
		if strings.HasPrefix(path, "/search/subject/") {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"list": []interface{}{}})
			return
		}
		http.Error(w, "unexpected "+r.Method+" "+path, http.StatusBadRequest)
	}
}
