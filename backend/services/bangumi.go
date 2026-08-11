package services

import (
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
	client *http.Client
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
