package database

import (
	"database/sql"

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
