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

// Drain 处理 outbox 中的待同步行。
// 全进程单槽：Drain 持 drainMu 处理完当前批次才释放。EnqueueWatched 与
// bangumi_me 触发的并发 Drain 会阻塞等待而非直接返回；上一轮 Drain 处理期间
// 新入队的行，由等锁的下一趟 Drain 重新 ListBangumiOutbox 取到并消费——行不丢。
// drainMu 全程持锁、只在函数返回时解锁一次：既保证互斥，也不会出现手动
// Unlock 后 panic 导致 unlock of unlocked mutex。网络 I/O 期间持锁，代价是
// 上游故障/慢响应时入队方会阻塞到本轮结束；所有调用方都运行在独立 goroutine
// （episode.go 的派发、bangumi_me.go 的 go func），阻塞不阻塞请求处理，
// 相比 TryLock 丢唤醒（后入队的行要等下一次入队才被消费）是更可取的取舍。
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
	s.drainRows(rows)
}

// drainRowCache 缓存 drainRows 内部调用的外部 API 结果，避免同一用户/番剧的
// 重复 HTTP 请求：用户身份校验（GetMe）每用户一次、剧集列表（ListSubjectEpisodes）
// 每用户+番剧一次、存在性校验（EnsureCollection）每用户+番剧一次。
//
// 注意：值可为 nil，表示缓存缺失或未执行；nil 不代表失败——失败路径直接 return 不
// 写入缓存，后续行仍会重试。调用方不得对 nil 值做业务判断。
type drainRowCache struct {
	// userGotMe 记录哪些用户已调过 GetMe 且成功。用 bool 而非 error，因为 GetMe 不
	// 返回错误语义的 nil（失败时直接 return err），成功时也不需要返回值。
	userGotMe map[int64]struct{}
	// episodeCache 缓存 bangumi 剧集列表：key = userID，value = bangumiID → episodes。
	// 同用户同一番剧的剧集列表不变，避免重复调用 ListSubjectEpisodes。
	episodeCache map[int64]map[int][]BangumiEpisode
	// collectionCache 缓存 EnsureCollection 的结果：key = userID → bangumiID。
	// 确保已创建收藏的番剧不再重复 POST。
	collectionCache map[int64]map[int]struct{}
}

func newDrainRowCache() *drainRowCache {
	return &drainRowCache{
		userGotMe:     make(map[int64]struct{}),
		episodeCache:  make(map[int64]map[int][]BangumiEpisode),
		collectionCache: make(map[int64]map[int]struct{}),
	}
}

// drainRows 在 Drain 持有 drainMu 期间被调用，内部不得触碰 drainMu。
func (s *BangumiSync) drainRows(rows []database.OutboxRow) {
	unauthorized := make(map[int64]bool)
	// tokenCache 按 userID 缓存令牌，避免同一用户多行 outbox 重复查库读取。
	tokenCache := make(map[int64]string)
	// 内部 API 结果缓存：避免同一用户/番剧重复调用。
	cache := newDrainRowCache()
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
		drainErr := s.drainRow(row, token, cache)
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

func (s *BangumiSync) drainRow(row database.OutboxRow, token string, cache *drainRowCache) error {
	// GetMe：每用户调用一次。
	if _, ok := cache.userGotMe[row.UserID]; !ok {
		if err := s.bangumi.GetMe(token); err != nil {
			return err
		}
		cache.userGotMe[row.UserID] = struct{}{}
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

	// ListSubjectEpisodes：每用户+番剧调用一次，结果在缓存中共享。
	if userCache, ok := cache.episodeCache[row.UserID]; ok {
		if episodes, ok := userCache[anime.BangumiID]; ok {
			bgmEpisode, ok := matchBangumiEpisode(*episode, episodes)
			if !ok {
				return database.DeleteBangumiOutbox(row.UserID, row.EpisodeID)
			}
			return s.patchEpisodeAndDelete(row, token, anime.BangumiID, bgmEpisode.ID)
		}
	} else {
		cache.episodeCache[row.UserID] = make(map[int][]BangumiEpisode)
	}

	bgmEpisodes, err := s.bangumi.ListSubjectEpisodes(token, anime.BangumiID)
	if err != nil {
		return err
	}
	cache.episodeCache[row.UserID][anime.BangumiID] = bgmEpisodes
	bgmEpisode, ok := matchBangumiEpisode(*episode, bgmEpisodes)
	if !ok {
		return database.DeleteBangumiOutbox(row.UserID, row.EpisodeID)
	}

	// EnsureCollection：每用户+番剧调用一次，避免重复 POST。
	if userColl, ok := cache.collectionCache[row.UserID]; ok {
		if _, already := userColl[anime.BangumiID]; !already {
			if err := s.bangumi.EnsureCollection(token, anime.BangumiID); err != nil {
				return err
			}
			if userColl == nil {
				cache.collectionCache[row.UserID] = map[int]struct{}{anime.BangumiID: {}}
			} else {
				userColl[anime.BangumiID] = struct{}{}
			}
		}
	} else {
		if err := s.bangumi.EnsureCollection(token, anime.BangumiID); err != nil {
			return err
		}
		cache.collectionCache[row.UserID] = map[int]struct{}{anime.BangumiID: {}}
	}
	return s.patchEpisodeAndDelete(row, token, anime.BangumiID, bgmEpisode.ID)
}

func (s *BangumiSync) patchEpisodeAndDelete(row database.OutboxRow, token string, bangumiID int, bgmEpisodeID int) error {
	if err := s.bangumi.PatchEpisodeCollection(token, bangumiID, []int{bgmEpisodeID}); err != nil {
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
