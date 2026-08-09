package database

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"fan-web/models"
)

const animeSelect = `
	SELECT id, title, title_cn, bangumi_id, cover, summary, ep_count, file_path, created_at
	FROM animes
`

const episodeSelect = `
	SELECT id, anime_id, ep_number, title, file_path, duration
	FROM episodes
`

type AnimeListItem struct {
	models.Anime
	WatchedCount int `json:"watched_count"`
}

func ListAnimes(page, pageSize int, keyword string, userID int64) ([]AnimeListItem, int, error) {
	var total int
	var rows *sql.Rows
	var err error
	offset := (page - 1) * pageSize
	selectSQL := `
		SELECT a.id, a.title, a.title_cn, a.bangumi_id, a.cover, a.summary, a.ep_count, a.file_path, a.created_at,
			(SELECT COUNT(*)
			 FROM watch_progress wp
			 JOIN episodes e ON wp.episode_id = e.id
			 WHERE e.anime_id = a.id AND wp.user_id = ? AND wp.watched = 1) AS watched_count
		FROM animes a`

	if keyword != "" {
		like := "%" + keyword + "%"
		if err := DB.QueryRow(
			"SELECT COUNT(*) FROM animes WHERE title LIKE ? OR title_cn LIKE ?",
			like, like,
		).Scan(&total); err != nil {
			return nil, 0, err
		}
		rows, err = DB.Query(
			selectSQL+" WHERE a.title LIKE ? OR a.title_cn LIKE ? ORDER BY a.created_at DESC, a.id DESC LIMIT ? OFFSET ?",
			userID, like, like, pageSize, offset,
		)
	} else {
		if err := DB.QueryRow("SELECT COUNT(*) FROM animes").Scan(&total); err != nil {
			return nil, 0, err
		}
		rows, err = DB.Query(
			selectSQL+" ORDER BY a.created_at DESC, a.id DESC LIMIT ? OFFSET ?",
			userID, pageSize, offset,
		)
	}
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	animes, err := scanAnimes(rows)
	return animes, total, err
}

func GetAnimeByID(id int64) (*models.Anime, error) {
	return scanAnime(DB.QueryRow(animeSelect+" WHERE id = ?", id))
}

func GetAnimeByBangumiID(bangumiID int) (*models.Anime, error) {
	anime, err := scanAnime(DB.QueryRow(animeSelect+" WHERE bangumi_id = ?", bangumiID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return anime, err
}

func GetEpisodeByID(id int64) (*models.Episode, error) {
	return scanEpisode(DB.QueryRow(episodeSelect+" WHERE id = ?", id))
}

func CreateAnime(anime *models.Anime) (*models.Anime, error) {
	result, err := DB.Exec(
		`INSERT INTO animes (title, title_cn, bangumi_id, cover, summary, ep_count, file_path)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		anime.Title, anime.TitleCn, anime.BangumiID, anime.Cover, anime.Summary, anime.EpCount, anime.FilePath,
	)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return GetAnimeByID(id)
}

func CreateEpisode(episode *models.Episode) error {
	_, err := DB.Exec(
		`INSERT INTO episodes (anime_id, ep_number, title, file_path, duration)
		 VALUES (?, ?, ?, ?, ?)`,
		episode.AnimeID, episode.EpNumber, episode.Title, episode.FilePath, episode.Duration,
	)
	return err
}

func UpdateAnime(anime *models.Anime) error {
	if _, err := GetAnimeByID(anime.ID); err != nil {
		return err
	}
	_, err := DB.Exec(
		`UPDATE animes SET title = ?, title_cn = ?, summary = ?, ep_count = ?, file_path = ? WHERE id = ?`,
		anime.Title, anime.TitleCn, anime.Summary, anime.EpCount, anime.FilePath, anime.ID,
	)
	return err
}

func DeleteAnime(id int64) error {
	result, err := DB.Exec("DELETE FROM animes WHERE id = ?", id)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func ListEpisodesByAnimeID(animeID int64) ([]models.Episode, error) {
	rows, err := DB.Query(episodeSelect+" WHERE anime_id = ? ORDER BY ep_number ASC, id ASC", animeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	episodes := make([]models.Episode, 0)
	for rows.Next() {
		episode, err := scanEpisode(rows)
		if err != nil {
			return nil, err
		}
		episodes = append(episodes, *episode)
	}
	return episodes, rows.Err()
}

// SyncEpisodes 以 (anime_id, ep_number) 为稳定业务键，将番剧剧集同步到给定列表。
// 匹配到的剧集仅更新字段并保留数据库 ID；新增集插入；未出现在输入中的旧集删除。
// 所有破坏性操作在单个事务内完成；任何一步失败都会整体回滚。
func SyncEpisodes(animeID int64, episodes []models.Episode) error {
	if animeID <= 0 {
		return fmt.Errorf("无效的番剧 ID")
	}
	seen := make(map[int]bool, len(episodes))
	for _, episode := range episodes {
		if episode.EpNumber <= 0 {
			return fmt.Errorf("无效的集数: %d", episode.EpNumber)
		}
		if strings.TrimSpace(episode.FilePath) == "" {
			return fmt.Errorf("集数 %d 缺少文件路径", episode.EpNumber)
		}
		if seen[episode.EpNumber] {
			return fmt.Errorf("输入中存在重复集数: %d", episode.EpNumber)
		}
		seen[episode.EpNumber] = true
	}

	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	existingRows, err := tx.Query(
		episodeSelect+" WHERE anime_id = ?", animeID,
	)
	if err != nil {
		return fmt.Errorf("查询已有剧集失败: %w", err)
	}
	existing := make(map[int]models.Episode)
	for existingRows.Next() {
		episode, err := scanEpisode(existingRows)
		if err != nil {
			existingRows.Close()
			return fmt.Errorf("读取已有剧集失败: %w", err)
		}
		if _, dup := existing[episode.EpNumber]; dup {
			existingRows.Close()
			return fmt.Errorf("数据库中存在重复集数 %d，请先清理后再重扫", episode.EpNumber)
		}
		existing[episode.EpNumber] = *episode
	}
	if err := existingRows.Err(); err != nil {
		existingRows.Close()
		return fmt.Errorf("遍历已有剧集失败: %w", err)
	}
	existingRows.Close()

	// 按集数升序处理，保证结果稳定。
	sortedInput := append([]models.Episode{}, episodes...)
	sort.Slice(sortedInput, func(i, j int) bool { return sortedInput[i].EpNumber < sortedInput[j].EpNumber })

	for _, episode := range sortedInput {
		if stored, ok := existing[episode.EpNumber]; ok {
			if _, err := tx.Exec(
				`UPDATE episodes SET title = ?, file_path = ?, duration = ? WHERE id = ? AND anime_id = ?`,
				episode.Title, episode.FilePath, episode.Duration, stored.ID, animeID,
			); err != nil {
				return fmt.Errorf("更新集数 %d 失败: %w", episode.EpNumber, err)
			}
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO episodes (anime_id, ep_number, title, file_path, duration) VALUES (?, ?, ?, ?, ?)`,
			animeID, episode.EpNumber, episode.Title, episode.FilePath, episode.Duration,
		); err != nil {
			return fmt.Errorf("新增集数 %d 失败: %w", episode.EpNumber, err)
		}
	}

	// 删除未出现在输入中的旧剧集。
	for epNumber, stored := range existing {
		if seen[epNumber] {
			continue
		}
		if _, err := tx.Exec("DELETE FROM episodes WHERE id = ?", stored.ID); err != nil {
			return fmt.Errorf("删除已移除集数 %d 失败: %w", epNumber, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交同步事务失败: %w", err)
	}
	return nil
}

func scanAnimes(rows *sql.Rows) ([]AnimeListItem, error) {
	animes := make([]AnimeListItem, 0)
	for rows.Next() {
		var item AnimeListItem
		var watchedCount int
		if err := rows.Scan(
			&item.ID, &item.Title, &item.TitleCn, &item.BangumiID,
			&item.Cover, &item.Summary, &item.EpCount, &item.FilePath, &item.CreatedAt,
			&watchedCount,
		); err != nil {
			return nil, err
		}
		item.WatchedCount = watchedCount
		animes = append(animes, item)
	}
	return animes, rows.Err()
}

func scanAnime(row scanner) (*models.Anime, error) {
	var anime models.Anime
	if err := row.Scan(
		&anime.ID, &anime.Title, &anime.TitleCn, &anime.BangumiID,
		&anime.Cover, &anime.Summary, &anime.EpCount, &anime.FilePath, &anime.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &anime, nil
}

func scanEpisode(row scanner) (*models.Episode, error) {
	var episode models.Episode
	if err := row.Scan(
		&episode.ID, &episode.AnimeID, &episode.EpNumber, &episode.Title, &episode.FilePath, &episode.Duration,
	); err != nil {
		return nil, err
	}
	return &episode, nil
}
