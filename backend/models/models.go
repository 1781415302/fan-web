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

type MatchCandidate struct {
	ID     int     `json:"id"`
	Name   string  `json:"name"`
	NameCn string  `json:"name_cn"`
	Score  float64 `json:"score"`
}

type UnidentifiedFile struct {
	ID         int64            `json:"id,omitempty"`
	FileName   string           `json:"file_name"`
	Reason     string           `json:"reason"`
	FilePath   string           `json:"file_path"`
	Candidates []MatchCandidate `json:"candidates"`
	UpdatedAt  time.Time        `json:"updated_at,omitempty"`
}

type ContinueItem struct {
	Anime     Anime     `json:"anime"`
	Episode   Episode   `json:"episode"`
	Position  int       `json:"position"`
	Watched   bool      `json:"watched"`
	UpdatedAt time.Time `json:"updated_at"`
}
