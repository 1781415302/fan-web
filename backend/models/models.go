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
	ID      int     `json:"id"`
	Name    string  `json:"name"`
	NameCn  string  `json:"name_cn"`
	Score   float64 `json:"score"`
	Year    int     `json:"year,omitempty"`
	EpCount int     `json:"ep_count,omitempty"`
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

type APIKey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	CreatedBy  int64      `json:"created_by"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// APIKeyCreated 仅在创建成功当次响应返回完整明文 Key。
type APIKeyCreated struct {
	APIKey
	Key string `json:"api_key"`
}

type AssistMatchRequest struct {
	FilePath      string   `json:"file_path"`
	FileNames     []string `json:"file_names,omitempty"`
	BangumiID     int      `json:"bangumi_id,omitempty"`
	TitleHint     string   `json:"title_hint,omitempty"`
	SeasonHint    int      `json:"season_hint,omitempty"`
	EpisodeOffset int      `json:"episode_offset,omitempty"`
	AutoCommit    *bool    `json:"auto_commit,omitempty"` // nil 默认视作 true
}

type AssistMatchBinding struct {
	FileName string `json:"file_name"`
	EpNumber int    `json:"ep_number"`
}

// Status 枚举："committed" | "matched" | "review_required" | "no_match"
type AssistMatchResponse struct {
	Status     string               `json:"status"`
	Reason     string               `json:"reason,omitempty"`
	Anime      *Anime               `json:"anime,omitempty"`
	Created    bool                 `json:"created,omitempty"`
	Scanned    int                  `json:"scanned,omitempty"`
	Episodes   []Episode            `json:"episodes,omitempty"`
	Candidate  *MatchCandidate      `json:"candidate,omitempty"`
	Candidates []MatchCandidate     `json:"candidates,omitempty"`
	Bindings   []AssistMatchBinding `json:"bindings,omitempty"`
	FilePath   string               `json:"file_path"`
	FileNames  []string             `json:"file_names,omitempty"`
}
