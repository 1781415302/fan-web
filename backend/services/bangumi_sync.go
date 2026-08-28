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
	token, ok, err := database.GetBangumiToken(userID)
	if err != nil {
		log.Printf("[BangumiSync] 读取令牌失败: %v", err)
		return
	}
	if !ok || token == "" {
		return
	}
	if err := database.EnqueueBangumiOutbox(userID, episodeID); err != nil {
		log.Printf("[BangumiSync] 入队失败: %v", err)
		return
	}
	s.Drain()
}

// Drain 处理 outbox 中的待同步行。
// 全进程单槽：已有 Drain 在进行就直接返回（EnqueueWatched 与 bangumi_me 都会
// 触发 Drain），避免第二趟在 HTTP 窗口内抢到锁重复处理同一行。
// drainMu 由本 goroutine 全程持有、只在函数返回时解锁一次：既保证互斥，
// 也不会出现手动 Unlock 后 panic 导致 unlock of unlocked mutex。
// 取行在持锁下完成，之后的 Bangumi 网络 I/O 与 DB 提交不再触碰这把锁，
// 且因单槽保证期间不会有第二趟介入，提交仍然安全。
func (s *BangumiSync) Drain() {
	if s == nil {
		return
	}
	if !s.drainMu.TryLock() {
		return
	}
	defer s.drainMu.Unlock()

	rows, err := database.ListBangumiOutbox(500)
	if err != nil {
		log.Printf("[BangumiSync] 读取 outbox 失败: %v", err)
		return
	}
	s.drainRows(rows)
}

// drainRows 在 Drain 持有 drainMu 期间被调用，内部不得触碰 drainMu。
func (s *BangumiSync) drainRows(rows []database.OutboxRow) {
	unauthorized := make(map[int64]bool)
	// tokenCache 按 userID 缓存令牌，避免同一用户多行 outbox 重复查库读取。
	tokenCache := make(map[int64]string)
	first := true
	for _, row := range rows {
		if unauthorized[row.UserID] {
			continue
		}
		token, ok := tokenCache[row.UserID]
		if !ok {
			tok, tokOk, tokErr := database.GetBangumiToken(row.UserID)
			if tokErr != nil {
				log.Printf("[BangumiSync] 读取令牌失败: %v", tokErr)
				continue
			}
			if !tokOk || tok == "" {
				if delErr := database.DeleteBangumiOutboxByUser(row.UserID); delErr != nil {
					log.Printf("[BangumiSync] 清除无令牌 outbox 失败: %v", delErr)
				}
				unauthorized[row.UserID] = true
				continue
			}
			token = tok
			tokenCache[row.UserID] = token
		}
		if !first {
			s.sleep()
		}
		first = false
		// 瞬时错误（非 401）保留行并额外让出时间片，避免上游故障期被持续
		// 触发的高频请求打爆 Bangumi。
		drainErr := s.drainRow(row, token)
		if drainErr != nil && !errors.Is(drainErr, ErrBangumiUnauthorized) {
			s.sleep()
		}
		if drainErr != nil {
			if errors.Is(drainErr, ErrBangumiUnauthorized) {
				if delErr := database.DeleteBangumiToken(row.UserID); delErr != nil {
					log.Printf("[BangumiSync] 清除令牌失败: %v", delErr)
				}
				if delErr := database.DeleteBangumiOutboxByUser(row.UserID); delErr != nil {
					log.Printf("[BangumiSync] 清除 outbox 失败: %v", delErr)
				}
				delete(tokenCache, row.UserID)
				unauthorized[row.UserID] = true
				continue
			}
			log.Printf("[BangumiSync] 出站同步失败: %v", drainErr)
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
