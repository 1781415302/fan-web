package database

import (
	"errors"
	"fmt"

	"fan-web/models"
)

// EpisodeRename 事务内一对一文件改名（旧路径→新路径）。
type EpisodeRename struct {
	EpisodeID int64
	From, To  string
}

// EpisodeReconcileInput 单番/全库共用的剧集认领事务输入。
type EpisodeReconcileInput struct {
	ExpectedAnime models.Anime     // 事务比 ID/BangumiID/FilePath/EpCount，不能只比 episodes
	Expected      []models.Episode // 开始扫描时读取的全量目标 anime 剧集，用于 CAS
	Desired       []models.Episode // 本次期望；新行 ID=0，已有行 ID 由事务按文件身份确定
	PresentFiles  []string
	Renames       []EpisodeRename
	RemoveMissing bool // 单番 v2=true，全库 v2=false；未解析不等于已删除
}

// 包级哨兵：调用方用 errors.Is 判定，勿依赖文案。
var (
	ErrEpisodeSnapshotChanged  = errors.New("剧集快照已变更")
	ErrEpisodeIdentityConflict = errors.New("剧集文件身份冲突")
)

// ReconcileEpisodes 按文件身份优先做 CAS 认领。S0 仅签名骨架；事务体属 S9。
func ReconcileEpisodes(animeID int64, input EpisodeReconcileInput) error {
	_ = input
	return fmt.Errorf("ReconcileEpisodes: 尚未实现（S9），animeID=%d", animeID)
}
