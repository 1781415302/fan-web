package database

import (
	"database/sql"
	"time"

	"fan-web/models"
)

const progressSelect = `
	SELECT id, user_id, episode_id, position, watched, updated_at
	FROM watch_progress
`

func GetProgress(userID, episodeID int64) (*models.WatchProgress, error) {
	progress := &models.WatchProgress{}
	if err := scanProgress(DB.QueryRow(
		progressSelect+" WHERE user_id = ? AND episode_id = ?",
		userID, episodeID,
	), progress); err != nil {
		if err == sql.ErrNoRows {
			return &models.WatchProgress{UserID: userID, EpisodeID: episodeID}, nil
		}
		return nil, err
	}
	return progress, nil
}

// UpsertProgress 写入观看进度。watched 字段不可逆：一旦为 true，
// 后续上报 watched=false 不会降级。position=0 且已有正进度时保留原值，
// 避免播放器销毁瞬间的 0 哨兵覆盖断点；首次 INSERT 的 0 仍会写入。
func UpsertProgress(userID, episodeID int64, position int, watched bool) error {
	watchedValue := 0
	if watched {
		watchedValue = 1
	}
	_, err := DB.Exec(`
		INSERT INTO watch_progress (user_id, episode_id, position, watched, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id, episode_id) DO UPDATE SET
			position = CASE
				WHEN excluded.position = 0 AND watch_progress.position > 0
				THEN watch_progress.position
				ELSE excluded.position
			END,
			watched = MAX(watch_progress.watched, excluded.watched),
			updated_at = excluded.updated_at
	`, userID, episodeID, position, watchedValue)
	return err
}

func ListProgressByAnime(userID, animeID int64) ([]models.WatchProgress, error) {
	rows, err := DB.Query(`
		SELECT wp.id, wp.user_id, wp.episode_id, wp.position, wp.watched, wp.updated_at
		FROM watch_progress wp
		JOIN episodes e ON wp.episode_id = e.id
		WHERE wp.user_id = ? AND e.anime_id = ?
		ORDER BY e.ep_number ASC, e.id ASC
	`, userID, animeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	progressList := make([]models.WatchProgress, 0)
	for rows.Next() {
		var progress models.WatchProgress
		if err := scanProgress(rows, &progress); err != nil {
			return nil, err
		}
		progressList = append(progressList, progress)
	}
	return progressList, rows.Err()
}

func CountWatchedByAnime(userID, animeID int64) (int, error) {
	var count int
	err := DB.QueryRow(`
		SELECT COUNT(*)
		FROM watch_progress wp
		JOIN episodes e ON wp.episode_id = e.id
		WHERE wp.user_id = ? AND e.anime_id = ? AND wp.watched = 1
	`, userID, animeID).Scan(&count)
	return count, err
}

func scanProgress(row scanner, progress *models.WatchProgress) error {
	var watched int
	if err := row.Scan(
		&progress.ID,
		&progress.UserID,
		&progress.EpisodeID,
		&progress.Position,
		&watched,
		&progress.UpdatedAt,
	); err != nil {
		return err
	}
	progress.Watched = watched != 0
	return nil
}

// ListContinueWatching 返回有进度的番剧，按该番 max(wp.updated_at) 降序。
// 每番用 PickContinueEpisode 选继续播放的集；全看完（nil）则跳过。
// limit 夹紧为 1..50，非法默认 20。
func ListContinueWatching(userID int64, limit int) ([]models.ContinueItem, error) {
	if limit < 1 {
		limit = 20
	} else if limit > 50 {
		limit = 50
	}

	rows, err := DB.Query(`
		SELECT a.id, MAX(wp.updated_at)
		FROM animes a
		JOIN episodes e ON e.anime_id = a.id
		JOIN watch_progress wp ON wp.episode_id = e.id
		WHERE wp.user_id = ?
		GROUP BY a.id
		ORDER BY MAX(wp.updated_at) DESC, a.id DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type animeActivity struct {
		id        int64
		updatedAt time.Time
	}
	activities := make([]animeActivity, 0)
	for rows.Next() {
		var item animeActivity
		var raw sql.NullString
		if err := rows.Scan(&item.id, &raw); err != nil {
			return nil, err
		}
		if raw.Valid {
			parsed, err := parseSQLiteDateTime(raw.String)
			if err != nil {
				return nil, err
			}
			item.updatedAt = parsed
		}
		activities = append(activities, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	items := make([]models.ContinueItem, 0)
	for _, activity := range activities {
		if len(items) >= limit {
			break
		}
		episodes, err := ListEpisodesByAnimeID(activity.id)
		if err != nil {
			return nil, err
		}
		progressList, err := ListProgressByAnime(userID, activity.id)
		if err != nil {
			return nil, err
		}
		picked := PickContinueEpisode(episodes, progressList)
		if picked == nil {
			continue
		}
		anime, err := GetAnimeByID(activity.id)
		if err != nil {
			return nil, err
		}
		position := 0
		watched := false
		for _, progress := range progressList {
			if progress.EpisodeID == picked.ID {
				position = progress.Position
				watched = progress.Watched
				break
			}
		}
		items = append(items, models.ContinueItem{
			Anime:     *anime,
			Episode:   *picked,
			Position:  position,
			Watched:   watched,
			UpdatedAt: activity.updatedAt,
		})
	}
	return items, nil
}

// PickContinueEpisode 与 Dart pickContinueEpisode 同序：先第一个
// !watched && position>0，再第一个 !watched，全看完返回 nil。
// episodes 必须已是 ListEpisodesByAnimeID 顺序，本函数不再排序。
func PickContinueEpisode(episodes []models.Episode, progress []models.WatchProgress) *models.Episode {
	byEpisode := make(map[int64]models.WatchProgress, len(progress))
	for _, item := range progress {
		byEpisode[item.EpisodeID] = item
	}
	for i := range episodes {
		item, ok := byEpisode[episodes[i].ID]
		if ok && !item.Watched && item.Position > 0 {
			episode := episodes[i]
			return &episode
		}
	}
	for i := range episodes {
		item, ok := byEpisode[episodes[i].ID]
		if !ok || !item.Watched {
			episode := episodes[i]
			return &episode
		}
	}
	return nil
}

func parseSQLiteDateTime(value string) (time.Time, error) {
	layouts := []string{
		"2006-01-02 15:04:05",
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
	}
	var lastErr error
	for _, layout := range layouts {
		parsed, err := time.ParseInLocation(layout, value, time.UTC)
		if err == nil {
			return parsed, nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}
