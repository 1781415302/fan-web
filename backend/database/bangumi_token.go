package database

import "database/sql"

type OutboxRow struct {
	UserID    int64
	EpisodeID int64
}

func SaveBangumiToken(userID int64, token string) error {
	_, err := DB.Exec(`
		INSERT INTO user_bangumi_tokens (user_id, access_token, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id) DO UPDATE SET
			access_token = excluded.access_token,
			updated_at = excluded.updated_at
	`, userID, token)
	return err
}

func GetBangumiToken(userID int64) (string, bool, error) {
	var token string
	err := DB.QueryRow(
		"SELECT access_token FROM user_bangumi_tokens WHERE user_id = ?", userID,
	).Scan(&token)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return token, true, nil
}

func DeleteBangumiToken(userID int64) error {
	_, err := DB.Exec("DELETE FROM user_bangumi_tokens WHERE user_id = ?", userID)
	return err
}

func DeleteBangumiOutboxByUser(userID int64) error {
	_, err := DB.Exec("DELETE FROM bangumi_sync_outbox WHERE user_id = ?", userID)
	return err
}

func EnqueueBangumiOutbox(userID, episodeID int64) error {
	_, err := DB.Exec(`
		INSERT INTO bangumi_sync_outbox (user_id, episode_id)
		VALUES (?, ?)
		ON CONFLICT(user_id, episode_id) DO NOTHING
	`, userID, episodeID)
	return err
}

func EnqueueWatchedForUser(userID int64) error {
	_, err := DB.Exec(`
		INSERT INTO bangumi_sync_outbox (user_id, episode_id)
		SELECT wp.user_id, wp.episode_id
		FROM watch_progress wp
		JOIN episodes e ON e.id = wp.episode_id
		JOIN animes a ON a.id = e.anime_id
		WHERE wp.user_id = ? AND wp.watched = 1 AND a.bangumi_id > 0
		ON CONFLICT(user_id, episode_id) DO NOTHING
	`, userID)
	return err
}

func ListBangumiOutbox(limit int) ([]OutboxRow, error) {
	rows, err := DB.Query(`
		SELECT user_id, episode_id
		FROM bangumi_sync_outbox
		ORDER BY created_at ASC, id ASC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]OutboxRow, 0)
	for rows.Next() {
		var row OutboxRow
		if err := rows.Scan(&row.UserID, &row.EpisodeID); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func DeleteBangumiOutbox(userID, episodeID int64) error {
	_, err := DB.Exec(
		"DELETE FROM bangumi_sync_outbox WHERE user_id = ? AND episode_id = ?",
		userID, episodeID,
	)
	return err
}
