package models

import "time"

type User struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Password  string    `json:"-"`
	IsAdmin   bool      `json:"is_admin"`
	CreatedAt time.Time `json:"created_at"`
}

type Anime struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	TitleCn   string    `json:"title_cn"`
	BangumiID int       `json:"bangumi_id"`
	Cover     string    `json:"cover"`
	Summary   string    `json:"summary"`
	EpCount   int       `json:"ep_count"`
	FilePath  string    `json:"file_path"`
	CreatedAt time.Time `json:"created_at"`
}

type Episode struct {
	ID       int64  `json:"id"`
	AnimeID  int64  `json:"anime_id"`
	EpNumber int    `json:"ep_number"`
	Title    string `json:"title"`
	FilePath string `json:"file_path"`
	Duration int    `json:"duration"`
}

type WatchProgress struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	EpisodeID int64     `json:"episode_id"`
	Position  int       `json:"position"`
	Watched   bool      `json:"watched"`
	UpdatedAt time.Time `json:"updated_at"`
}
