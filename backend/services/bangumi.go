package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrBangumiNotFound = errors.New("未找到该条目")
var (
	ErrBangumiUnauthorized = errors.New("Bangumi 令牌无效")
	ErrBangumiRateLimited  = errors.New("Bangumi 请求过于频繁")
	ErrBangumiBadRequest   = errors.New("Bangumi 请求无效")
)

const (
	bangumiBaseURL = "https://api.bgm.tv"
	bangumiUA      = "fan-web/1.0 (private anime library)"
	// maxBangumiResponseBytes 限制 Bangumi API 响应大小，防止异常响应无界占用内存。
	maxBangumiResponseBytes = 4 << 20 // 4 MiB
)

type BangumiSearchItem struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	NameCn   string `json:"name_cn"`
	Summary  string `json:"summary"`
	EpsCount int    `json:"eps_count"`
	Cover    string `json:"cover"`
}

type BangumiSubjectInfo struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	NameCn        string `json:"name_cn"`
	Summary       string `json:"summary"`
	Cover         string `json:"cover"`
	TotalEpisodes int    `json:"total_episodes"`
}

type bgmSearchResponse struct {
	List []bgmSearchRaw `json:"list"`
}

type bgmSearchRaw struct {
	ID       int       `json:"id"`
	Name     string    `json:"name"`
	NameCn   string    `json:"name_cn"`
	Summary  string    `json:"summary"`
	EpsCount int       `json:"eps_count"`
	Eps      int       `json:"eps"`
	Images   bgmImages `json:"images"`
}

type bgmSubjectRaw struct {
	ID            int       `json:"id"`
	Name          string    `json:"name"`
	NameCn        string    `json:"name_cn"`
	Summary       string    `json:"summary"`
	Images        bgmImages `json:"images"`
	TotalEpisodes int       `json:"total_episodes"`
	Eps           int       `json:"eps"`
}

type bgmImages struct {
	Large  string `json:"large"`
	Common string `json:"common"`
	Medium string `json:"medium"`
	Small  string `json:"small"`
	Grid   string `json:"grid"`
}

type BangumiService struct {
	client  *http.Client
	baseURL string
}

func NewBangumiService() *BangumiService {
	return &BangumiService{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *BangumiService) Search(keyword string) ([]BangumiSearchItem, error) {
	apiURL := fmt.Sprintf("%s/search/subject/%s?type=2&responseGroup=small",
		bangumiBaseURL, url.PathEscape(strings.TrimSpace(keyword)))

	var response bgmSearchResponse
	if err := s.doRequest(apiURL, &response); err != nil {
		return nil, err
	}

	items := make([]BangumiSearchItem, 0, len(response.List))
	for _, item := range response.List {
		epsCount := item.EpsCount
		if epsCount <= 0 {
			epsCount = item.Eps
		}
		items = append(items, BangumiSearchItem{
			ID:       item.ID,
			Name:     item.Name,
			NameCn:   item.NameCn,
			Summary:  item.Summary,
			EpsCount: epsCount,
			Cover:    toHTTPS(pickCover(item.Images)),
		})
	}
	return items, nil
}

func (s *BangumiService) GetSubject(id int) (*BangumiSubjectInfo, error) {
	apiURL := fmt.Sprintf("%s/v0/subjects/%d", bangumiBaseURL, id)

	var subject bgmSubjectRaw
	if err := s.doRequest(apiURL, &subject); err != nil {
		return nil, err
	}

	totalEpisodes := subject.TotalEpisodes
	if totalEpisodes <= 0 {
		totalEpisodes = subject.Eps
	}
	return &BangumiSubjectInfo{
		ID:            subject.ID,
		Name:          subject.Name,
		NameCn:        subject.NameCn,
		Summary:       subject.Summary,
		Cover:         toHTTPS(pickCover(subject.Images)),
		TotalEpisodes: totalEpisodes,
	}, nil
}

const (
	subjectEpisodePageLimit    = 200
	episodeCollectionPageLimit = 1000
)

// BangumiEpisode 是上游章节（本篇 type=0）的映射字段。
type BangumiEpisode struct {
	ID   int     `json:"id"`
	Ep   float64 `json:"ep"`
	Sort float64 `json:"sort"`
}

// BangumiEpisodeCollection 是用户章节收藏行。Type 2 = 看过。
type BangumiEpisodeCollection struct {
	Episode BangumiEpisode `json:"episode"`
	Type    int            `json:"type"`
}

type bgmEpisodePage struct {
	Data []BangumiEpisode `json:"data"`
}

type bgmEpisodeCollectionPage struct {
	Data []BangumiEpisodeCollection `json:"data"`
}

// SetBaseURL 把上游指到测试服务器。生产路径保持默认 https://api.bgm.tv。
func (s *BangumiService) SetBaseURL(baseURL string) {
	s.baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

func (s *BangumiService) endpoint(path string) string {
	base := bangumiBaseURL
	if s != nil && s.baseURL != "" {
		base = s.baseURL
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.TrimRight(base, "/") + path
}

func (s *BangumiService) GetMe(token string) error {
	return s.doAuthRequest(http.MethodGet, "/v0/me", token, nil, &struct{}{})
}

func (s *BangumiService) ListSubjectEpisodes(token string, subjectID int) ([]BangumiEpisode, error) {
	all := make([]BangumiEpisode, 0)
	offset := 0
	for {
		path := fmt.Sprintf("/v0/episodes?subject_id=%d&type=0&limit=%d&offset=%d",
			subjectID, subjectEpisodePageLimit, offset)
		var page bgmEpisodePage
		if err := s.doAuthRequest(http.MethodGet, path, token, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		if len(page.Data) < subjectEpisodePageLimit {
			break
		}
		offset += len(page.Data)
	}
	return all, nil
}

func (s *BangumiService) EnsureCollection(token string, subjectID int) error {
	path := fmt.Sprintf("/v0/users/-/collections/%d", subjectID)
	err := s.doAuthRequest(http.MethodGet, path, token, nil, &struct{}{})
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrBangumiNotFound) {
		return err
	}
	postErr := s.doAuthRequest(http.MethodPost, path, token, map[string]int{"type": 3}, nil)
	if postErr == nil || errors.Is(postErr, ErrBangumiBadRequest) {
		return nil
	}
	return postErr
}

func (s *BangumiService) PatchEpisodeCollection(token string, subjectID int, episodeIDs []int) error {
	path := fmt.Sprintf("/v0/users/-/collections/%d/episodes", subjectID)
	body := map[string]interface{}{
		"episode_id": episodeIDs,
		"type":       2,
	}
	return s.doAuthRequest(http.MethodPatch, path, token, body, nil)
}

func (s *BangumiService) ListEpisodeCollection(token string, subjectID int) ([]BangumiEpisodeCollection, error) {
	all := make([]BangumiEpisodeCollection, 0)
	offset := 0
	for {
		path := fmt.Sprintf("/v0/users/-/collections/%d/episodes?episode_type=0&limit=%d&offset=%d",
			subjectID, episodeCollectionPageLimit, offset)
		var page bgmEpisodeCollectionPage
		if err := s.doAuthRequest(http.MethodGet, path, token, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		if len(page.Data) < episodeCollectionPageLimit {
			break
		}
		offset += len(page.Data)
	}
	return all, nil
}

func (s *BangumiService) doAuthRequest(method, path, token string, body any, target interface{}) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, s.endpoint(path), reader)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", bangumiUA)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	response, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("请求 Bangumi API 失败: %w", err)
	}
	defer response.Body.Close()

	limited := io.LimitReader(response.Body, maxBangumiResponseBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("读取 Bangumi API 响应失败: %w", err)
	}
	if len(raw) > maxBangumiResponseBytes {
		return fmt.Errorf("Bangumi API 响应过大，超过 %d 字节上限", maxBangumiResponseBytes)
	}

	switch response.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		if target == nil || response.StatusCode == http.StatusNoContent || len(raw) == 0 {
			return nil
		}
		if err := json.Unmarshal(raw, target); err != nil {
			return fmt.Errorf("解析 Bangumi 响应失败: %w", err)
		}
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrBangumiUnauthorized
	case http.StatusNotFound:
		return ErrBangumiNotFound
	case http.StatusBadRequest:
		return ErrBangumiBadRequest
	case http.StatusTooManyRequests:
		return ErrBangumiRateLimited
	default:
		return fmt.Errorf("Bangumi API 返回状态码 %d", response.StatusCode)
	}
}

func (s *BangumiService) doRequest(apiURL string, target interface{}) error {
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", bangumiUA)

	response, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("请求 Bangumi API 失败: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return ErrBangumiNotFound
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Bangumi API 返回状态码 %d", response.StatusCode)
	}

	// 限制响应大小（与 updater 中 maxChecksumBytes 同思路），
	// 防止上游异常超大响应无界占用内存。
	limited := io.LimitReader(response.Body, maxBangumiResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("读取 Bangumi API 响应失败: %w", err)
	}
	if len(body) > maxBangumiResponseBytes {
		return fmt.Errorf("Bangumi API 响应过大，超过 %d 字节上限", maxBangumiResponseBytes)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("解析 Bangumi 响应失败: %w", err)
	}
	return nil
}

func pickCover(images bgmImages) string {
	for _, cover := range []string{images.Large, images.Common, images.Medium, images.Small, images.Grid} {
		if cover != "" {
			return cover
		}
	}
	return ""
}

func toHTTPS(rawURL string) string {
	return strings.Replace(rawURL, "http://", "https://", 1)
}
