package services

import (
	"database/sql"
	"errors"
	"log"
	"sync"
	"time"

	"fan-web/database"
	"fan-web/models"
)

const defaultDrainInterval = 350 * time.Millisecond

type BangumiSync struct {
	bangumi  *BangumiService
	interval time.Duration
	drainMu  sync.Mutex
}

func NewBangumiSync(bangumi *BangumiService) *BangumiSync {
	return &BangumiSync{
		bangumi:  bangumi,
		interval: defaultDrainInterval,
	}
}

type SyncResult struct {
	Animes         int `json:"animes"`
	EpisodesMarked int `json:"episodes_marked"`
}

func bangumiEpisodeNumber(ep BangumiEpisode) int {
	if ep.Ep == 0 {
		return int(ep.Sort)
	}
	return int(ep.Ep)
}

func matchBangumiEpisode(local models.Episode, episodes []BangumiEpisode) (BangumiEpisode, bool) {
	for _, item := range episodes {
		if local.EpNumber == bangumiEpisodeNumber(item) {
			return item, true
		}
	}
	return BangumiEpisode{}, false
}

func matchLocalEpisode(episodes []models.Episode, bgm BangumiEpisode) (models.Episode, bool) {
	target := bangumiEpisodeNumber(bgm)
	for _, item := range episodes {
		if item.EpNumber == target {
			return item, true
		}
	}
	return models.Episode{}, false
}

func (s *BangumiSync) EnqueueWatched(userID, episodeID int64) {
	if s == nil {
		return
	}
	if err := database.EnqueueBangumiOutbox(userID, episodeID); err != nil {
		log.Printf("[BangumiSync] 入队失败: %v", err)
		return
	}
	s.Drain()
}

func (s *BangumiSync) Drain() {
	if s == nil {
		return
	}
	s.drainMu.Lock()
	defer s.drainMu.Unlock()

	rows, err := database.ListBangumiOutbox(500)
	if err != nil {
		log.Printf("[BangumiSync] 读取 outbox 失败: %v", err)
		return
	}

	unauthorized := make(map[int64]bool)
	first := true
	for _, row := range rows {
		if unauthorized[row.UserID] {
			continue
		}
		token, ok, err := database.GetBangumiToken(row.UserID)
		if err != nil {
			log.Printf("[BangumiSync] 读取令牌失败: %v", err)
			continue
		}
		if !ok || token == "" {
			continue
		}
		if !first {
			s.sleep()
		}
		first = false
		if err := s.drainRow(row, token); err != nil {
			if errors.Is(err, ErrBangumiUnauthorized) {
				if delErr := database.DeleteBangumiToken(row.UserID); delErr != nil {
					log.Printf("[BangumiSync] 清除令牌失败: %v", delErr)
				}
				if delErr := database.DeleteBangumiOutboxByUser(row.UserID); delErr != nil {
					log.Printf("[BangumiSync] 清除 outbox 失败: %v", delErr)
				}
				unauthorized[row.UserID] = true
				continue
			}
			log.Printf("[BangumiSync] 出站同步失败: %v", err)
		}
	}
}

func (s *BangumiSync) sleep() {
	if s != nil && s.interval > 0 {
		time.Sleep(s.interval)
	}
}

func (s *BangumiSync) drainRow(row database.OutboxRow, token string) error {
	if err := s.bangumi.GetMe(token); err != nil {
		return err
	}
	episode, err := database.GetEpisodeByID(row.EpisodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.DeleteBangumiOutbox(row.UserID, row.EpisodeID)
		}
		return err
	}
	anime, err := database.GetAnimeByID(episode.AnimeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.DeleteBangumiOutbox(row.UserID, row.EpisodeID)
		}
		return err
	}
	if anime.BangumiID <= 0 {
		return database.DeleteBangumiOutbox(row.UserID, row.EpisodeID)
	}

	bgmEpisodes, err := s.bangumi.ListSubjectEpisodes(token, anime.BangumiID)
	if err != nil {
		return err
	}
	bgmEpisode, ok := matchBangumiEpisode(*episode, bgmEpisodes)
	if !ok {
		return database.DeleteBangumiOutbox(row.UserID, row.EpisodeID)
	}
	if err := s.bangumi.EnsureCollection(token, anime.BangumiID); err != nil {
		return err
	}
	if err := s.bangumi.PatchEpisodeCollection(token, anime.BangumiID, []int{bgmEpisode.ID}); err != nil {
		return err
	}
	return database.DeleteBangumiOutbox(row.UserID, row.EpisodeID)
}

func (s *BangumiSync) SyncInbound(userID int64) (*SyncResult, error) {
	token, ok, err := database.GetBangumiToken(userID)
	if err != nil {
		return nil, err
	}
	if !ok || token == "" {
		return nil, ErrBangumiUnauthorized
	}
	if err := s.bangumi.GetMe(token); err != nil {
		if errors.Is(err, ErrBangumiUnauthorized) {
			_ = database.DeleteBangumiToken(userID)
			_ = database.DeleteBangumiOutboxByUser(userID)
		}
		return nil, err
	}

	result := &SyncResult{}
	page := 1
	for {
		items, total, err := database.ListAnimes(page, 100, "", userID)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if item.BangumiID <= 0 {
				continue
			}
			marked, err := s.syncAnimeInbound(userID, token, item.Anime)
			if err != nil {
				if errors.Is(err, ErrBangumiUnauthorized) {
					_ = database.DeleteBangumiToken(userID)
					_ = database.DeleteBangumiOutboxByUser(userID)
					return nil, err
				}
				if errors.Is(err, ErrBangumiRateLimited) {
					log.Printf("[BangumiSync] 入站 429，跳过 bangumi_id=%d", item.BangumiID)
					continue
				}
				return nil, err
			}
			result.Animes++
			result.EpisodesMarked += marked
		}
		if len(items) == 0 || page*100 >= total {
			break
		}
		page++
	}
	return result, nil
}

func (s *BangumiSync) syncAnimeInbound(userID int64, token string, anime models.Anime) (int, error) {
	collections, err := s.bangumi.ListEpisodeCollection(token, anime.BangumiID)
	if err != nil {
		if errors.Is(err, ErrBangumiNotFound) {
			return 0, nil
		}
		return 0, err
	}
	locals, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		return 0, err
	}
	marked := 0
	for _, item := range collections {
		if item.Type != 2 {
			continue
		}
		local, ok := matchLocalEpisode(locals, item.Episode)
		if !ok {
			continue
		}
		if err := database.UpsertProgress(userID, local.ID, 0, true); err != nil {
			return marked, err
		}
		marked++
	}
	return marked, nil
}
