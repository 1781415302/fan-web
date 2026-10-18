package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

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

// reconcileCommitHook 仅供测试注入故障：若非 nil，在写完提交前调用；
// 返回错误则 rollback，事务外不暴露中间编号。
var reconcileCommitHook func() error

type reconcileUpdate struct {
	id       int64
	epNumber int
	filePath string
}

type reconcileInsert struct {
	epNumber int
	title    string
	filePath string
	duration int
}

// ReconcileEpisodes 按文件身份优先做 CAS 认领（契约 4.4）。
func ReconcileEpisodes(animeID int64, input EpisodeReconcileInput) error {
	if err := validateReconcileInput(animeID, input); err != nil {
		return err
	}

	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	anime, err := scanAnime(tx.QueryRow(animeSelect+" WHERE id = ?", animeID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrEpisodeSnapshotChanged
		}
		return fmt.Errorf("读取番剧失败: %w", err)
	}
	if !animeCASEqual(*anime, input.ExpectedAnime) || anime.ID != animeID {
		return ErrEpisodeSnapshotChanged
	}

	oldEps, err := listEpisodesTx(tx, animeID)
	if err != nil {
		return err
	}
	if !episodeCASEqual(oldEps, input.Expected) {
		return ErrEpisodeSnapshotChanged
	}

	updates, inserts, deletes, err := planReconcile(oldEps, input)
	if err != nil {
		return err
	}

	// 跨 anime 认领复查：Desired 最终路径不得已被同目录其他番剧占用。
	for _, u := range updates {
		if err := checkCrossAnimeClaimTx(tx, animeID, anime.FilePath, u.filePath); err != nil {
			return err
		}
	}
	for _, ins := range inserts {
		if err := checkCrossAnimeClaimTx(tx, animeID, anime.FilePath, ins.filePath); err != nil {
			return err
		}
	}

	if err := applyReconcileWrites(tx, animeID, oldEps, updates, inserts, deletes); err != nil {
		return err
	}

	if reconcileCommitHook != nil {
		if err := reconcileCommitHook(); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交认领事务失败: %w", err)
	}
	return nil
}

func validateReconcileInput(animeID int64, input EpisodeReconcileInput) error {
	if animeID <= 0 {
		return fmt.Errorf("无效的番剧 ID")
	}
	if input.ExpectedAnime.ID != 0 && input.ExpectedAnime.ID != animeID {
		return fmt.Errorf("ExpectedAnime.ID 与 animeID 不一致")
	}

	presentSeen := make(map[string]struct{}, len(input.PresentFiles))
	for _, name := range input.PresentFiles {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("PresentFiles 含空文件名")
		}
		if _, dup := presentSeen[name]; dup {
			return fmt.Errorf("PresentFiles 存在重复文件名: %s", name)
		}
		presentSeen[name] = struct{}{}
	}

	pathSeen := make(map[string]struct{}, len(input.Desired))
	numSeen := make(map[int]struct{}, len(input.Desired))
	for _, ep := range input.Desired {
		if ep.EpNumber <= 0 {
			return fmt.Errorf("无效的集数: %d", ep.EpNumber)
		}
		if strings.TrimSpace(ep.FilePath) == "" {
			return fmt.Errorf("集数 %d 缺少文件路径", ep.EpNumber)
		}
		if _, dup := pathSeen[ep.FilePath]; dup {
			return fmt.Errorf("Desired 存在重复文件路径: %s", ep.FilePath)
		}
		pathSeen[ep.FilePath] = struct{}{}
		if _, dup := numSeen[ep.EpNumber]; dup {
			return fmt.Errorf("Desired 存在重复集数: %d", ep.EpNumber)
		}
		numSeen[ep.EpNumber] = struct{}{}
	}

	renameIDs := make(map[int64]struct{}, len(input.Renames))
	renameTos := make(map[string]struct{}, len(input.Renames))
	for _, r := range input.Renames {
		if r.EpisodeID <= 0 {
			return fmt.Errorf("Rename 缺少有效 EpisodeID")
		}
		if strings.TrimSpace(r.From) == "" || strings.TrimSpace(r.To) == "" {
			return fmt.Errorf("Rename From/To 不得为空")
		}
		if r.From == r.To {
			return fmt.Errorf("Rename From 与 To 相同: %s", r.From)
		}
		if _, dup := renameIDs[r.EpisodeID]; dup {
			return fmt.Errorf("Rename 存在重复 EpisodeID: %d", r.EpisodeID)
		}
		renameIDs[r.EpisodeID] = struct{}{}
		if _, dup := renameTos[r.To]; dup {
			return fmt.Errorf("Rename 存在重复目标路径: %s", r.To)
		}
		renameTos[r.To] = struct{}{}
	}
	return nil
}

func animeCASEqual(actual, expected models.Anime) bool {
	return actual.ID == expected.ID &&
		actual.BangumiID == expected.BangumiID &&
		actual.FilePath == expected.FilePath &&
		actual.EpCount == expected.EpCount
}

func episodeCASEqual(actual, expected []models.Episode) bool {
	if len(actual) != len(expected) {
		return false
	}
	type key struct {
		id       int64
		path     string
		epNumber int
	}
	counts := make(map[key]int, len(actual))
	for _, ep := range actual {
		counts[key{ep.ID, ep.FilePath, ep.EpNumber}]++
	}
	for _, ep := range expected {
		k := key{ep.ID, ep.FilePath, ep.EpNumber}
		if counts[k] == 0 {
			return false
		}
		counts[k]--
	}
	return true
}

func listEpisodesTx(tx *sql.Tx, animeID int64) ([]models.Episode, error) {
	rows, err := tx.Query(episodeSelect+" WHERE anime_id = ? ORDER BY ep_number ASC, id ASC", animeID)
	if err != nil {
		return nil, fmt.Errorf("查询已有剧集失败: %w", err)
	}
	defer rows.Close()

	episodes := make([]models.Episode, 0)
	for rows.Next() {
		episode, err := scanEpisode(rows)
		if err != nil {
			return nil, fmt.Errorf("读取已有剧集失败: %w", err)
		}
		episodes = append(episodes, *episode)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历已有剧集失败: %w", err)
	}
	return episodes, nil
}

func planReconcile(oldEps []models.Episode, input EpisodeReconcileInput) ([]reconcileUpdate, []reconcileInsert, []int64, error) {
	present := make(map[string]struct{}, len(input.PresentFiles))
	for _, name := range input.PresentFiles {
		present[name] = struct{}{}
	}

	oldByID := make(map[int64]models.Episode, len(oldEps))
	oldByPath := make(map[string]models.Episode, len(oldEps))
	for _, ep := range oldEps {
		if _, dup := oldByPath[ep.FilePath]; dup {
			return nil, nil, nil, fmt.Errorf("数据库中存在重复文件路径 %s，请先清理后再重扫", ep.FilePath)
		}
		oldByID[ep.ID] = ep
		oldByPath[ep.FilePath] = ep
	}

	// 校验 Rename，并建立 EpisodeID → To。
	renameToByID := make(map[int64]string, len(input.Renames))
	renameIDByTo := make(map[string]int64, len(input.Renames))
	for _, r := range input.Renames {
		old, ok := oldByID[r.EpisodeID]
		if !ok {
			return nil, nil, nil, fmt.Errorf("%w: Rename 指向未知剧集 %d", ErrEpisodeIdentityConflict, r.EpisodeID)
		}
		if old.FilePath != r.From {
			return nil, nil, nil, fmt.Errorf("%w: Rename From 与旧路径不符", ErrEpisodeIdentityConflict)
		}
		if _, stillThere := present[r.From]; stillThere {
			return nil, nil, nil, fmt.Errorf("%w: Rename From 仍在 PresentFiles", ErrEpisodeIdentityConflict)
		}
		if _, ok := present[r.To]; !ok {
			return nil, nil, nil, fmt.Errorf("%w: Rename To 不在 PresentFiles", ErrEpisodeIdentityConflict)
		}
		// To 若已是另一旧行的当前路径（且该文件仍在），会与同文件匹配冲突。
		if other, ok := oldByPath[r.To]; ok && other.ID != r.EpisodeID {
			if _, still := present[r.To]; still {
				return nil, nil, nil, fmt.Errorf("%w: Rename To 与同文件匹配冲突", ErrEpisodeIdentityConflict)
			}
		}
		renameToByID[r.EpisodeID] = r.To
		renameIDByTo[r.To] = r.EpisodeID
	}

	claimed := make(map[int64]struct{}, len(input.Desired))
	updates := make([]reconcileUpdate, 0, len(input.Desired))
	inserts := make([]reconcileInsert, 0)

	for _, d := range input.Desired {
		// 1) 同文件名优先匹配。
		if old, ok := oldByPath[d.FilePath]; ok {
			if d.ID != 0 && d.ID != old.ID {
				return nil, nil, nil, fmt.Errorf("%w: Desired 为同名文件指定了他人 ID", ErrEpisodeIdentityConflict)
			}
			if _, taken := claimed[old.ID]; taken {
				return nil, nil, nil, fmt.Errorf("%w: 剧集 ID %d 被重复认领", ErrEpisodeIdentityConflict, old.ID)
			}
			claimed[old.ID] = struct{}{}
			updates = append(updates, reconcileUpdate{
				id:       old.ID,
				epNumber: d.EpNumber,
				filePath: old.FilePath, // 路径不变；只改需要更正的集号
			})
			continue
		}

		// 2) 合法 Rename 目标。
		if srcID, ok := renameIDByTo[d.FilePath]; ok {
			if d.ID != 0 && d.ID != srcID {
				return nil, nil, nil, fmt.Errorf("%w: Desired 为 Rename 目标指定了他人 ID", ErrEpisodeIdentityConflict)
			}
			if _, taken := claimed[srcID]; taken {
				return nil, nil, nil, fmt.Errorf("%w: 剧集 ID %d 被重复认领", ErrEpisodeIdentityConflict, srcID)
			}
			claimed[srcID] = struct{}{}
			updates = append(updates, reconcileUpdate{
				id:       srcID,
				epNumber: d.EpNumber,
				filePath: d.FilePath,
			})
			continue
		}

		// 3) 新文件：不得挂他人 ID；不得与「缺失旧行同号」在无 Rename 时复用。
		if d.ID != 0 {
			if old, ok := oldByID[d.ID]; ok {
				return nil, nil, nil, fmt.Errorf("%w: 新文件不得复用旧 ID %d（旧路径 %s）", ErrEpisodeIdentityConflict, d.ID, old.FilePath)
			}
			return nil, nil, nil, fmt.Errorf("%w: Desired 指定了不存在的剧集 ID %d", ErrEpisodeIdentityConflict, d.ID)
		}
		for _, old := range oldEps {
			if old.EpNumber != d.EpNumber {
				continue
			}
			if _, still := present[old.FilePath]; still {
				continue
			}
			// 缺失旧行同号：仅当该旧行被 Rename 认领到本 Desired 才合法；此处已排除 Rename。
			if _, taken := claimed[old.ID]; !taken {
				if _, renamed := renameToByID[old.ID]; !renamed {
					return nil, nil, nil, fmt.Errorf("%w: 缺失旧行与新路径同号且无合法 Rename", ErrEpisodeIdentityConflict)
				}
			}
		}
		inserts = append(inserts, reconcileInsert{
			epNumber: d.EpNumber,
			title:    d.Title,
			filePath: d.FilePath,
			duration: d.Duration,
		})
	}

	// Rename 必须被 Desired 消费（To 出现在 Desired）。
	for id, to := range renameToByID {
		if _, ok := claimed[id]; !ok {
			return nil, nil, nil, fmt.Errorf("%w: Rename 目标 %s 未进入 Desired", ErrEpisodeIdentityConflict, to)
		}
	}

	deletes := make([]int64, 0)
	for _, old := range oldEps {
		if _, ok := claimed[old.ID]; ok {
			continue
		}
		if _, still := present[old.FilePath]; still {
			return nil, nil, nil, fmt.Errorf("%w: 文件仍存在却未进入 Desired: %s", ErrEpisodeIdentityConflict, old.FilePath)
		}
		if input.RemoveMissing {
			deletes = append(deletes, old.ID)
			continue
		}
		// RemoveMissing=false：保留缺失旧行；其集号不得与 Desired/更新后冲突。
		for _, u := range updates {
			if u.epNumber == old.EpNumber {
				return nil, nil, nil, fmt.Errorf("%w: 保留的缺失旧行与 Desired 集号冲突", ErrEpisodeIdentityConflict)
			}
		}
		for _, ins := range inserts {
			if ins.epNumber == old.EpNumber {
				return nil, nil, nil, fmt.Errorf("%w: 保留的缺失旧行与新文件集号冲突", ErrEpisodeIdentityConflict)
			}
		}
	}

	return updates, inserts, deletes, nil
}

func checkCrossAnimeClaimTx(tx *sql.Tx, animeID int64, animeDir, fileName string) error {
	var otherID int64
	err := tx.QueryRow(`
		SELECT ep.anime_id
		FROM episodes ep
		JOIN animes a ON ep.anime_id = a.id
		WHERE REPLACE(ep.file_path, '\', '/') = REPLACE(?, '\', '/')
		  AND REPLACE(a.file_path, '\', '/') = REPLACE(?, '\', '/')
		  AND ep.anime_id != ?
		LIMIT 1
	`, fileName, animeDir, animeID).Scan(&otherID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("复查跨番剧认领失败: %w", err)
	}
	return fmt.Errorf("%w: 文件已被其他番剧认领", ErrEpisodeIdentityConflict)
}

func applyReconcileWrites(
	tx *sql.Tx,
	animeID int64,
	oldEps []models.Episode,
	updates []reconcileUpdate,
	inserts []reconcileInsert,
	deletes []int64,
) error {
	oldByID := make(map[int64]models.Episode, len(oldEps))
	usedNumbers := make(map[int]struct{}, len(oldEps))
	for _, ep := range oldEps {
		oldByID[ep.ID] = ep
		usedNumbers[ep.EpNumber] = struct{}{}
	}

	// 需要改集号的行先落到未占用的临时负编号，再写最终正编号（交换/换号安全）。
	type pendingFinal struct {
		id       int64
		epNumber int
		filePath string
	}
	pending := make([]pendingFinal, 0, len(updates))
	nextTemp := int64(-1)

	for _, u := range updates {
		old := oldByID[u.id]
		needNumberChange := old.EpNumber != u.epNumber
		needPathChange := old.FilePath != u.filePath
		if !needNumberChange && !needPathChange {
			continue
		}
		if needNumberChange {
			for {
				temp := int(nextTemp)
				nextTemp--
				if _, used := usedNumbers[temp]; used {
					continue
				}
				usedNumbers[temp] = struct{}{}
				if _, err := tx.Exec(
					`UPDATE episodes SET ep_number = ? WHERE id = ? AND anime_id = ?`,
					temp, u.id, animeID,
				); err != nil {
					return fmt.Errorf("写入临时集号失败 (id=%d): %w", u.id, err)
				}
				break
			}
		}
		pending = append(pending, pendingFinal{id: u.id, epNumber: u.epNumber, filePath: u.filePath})
	}

	// 先删除缺失行，释放集号，再落到最终正编号。
	for _, id := range deletes {
		if _, err := tx.Exec(`DELETE FROM episodes WHERE id = ? AND anime_id = ?`, id, animeID); err != nil {
			return fmt.Errorf("删除缺失剧集 id=%d 失败: %w", id, err)
		}
	}

	for _, p := range pending {
		if _, err := tx.Exec(
			`UPDATE episodes SET ep_number = ?, file_path = ? WHERE id = ? AND anime_id = ?`,
			p.epNumber, p.filePath, p.id, animeID,
		); err != nil {
			return fmt.Errorf("更新剧集 id=%d 失败: %w", p.id, err)
		}
	}

	for _, ins := range inserts {
		if _, err := tx.Exec(
			`INSERT INTO episodes (anime_id, ep_number, title, file_path, duration) VALUES (?, ?, ?, ?, ?)`,
			animeID, ins.epNumber, ins.title, ins.filePath, ins.duration,
		); err != nil {
			return fmt.Errorf("新增集数 %d 失败: %w", ins.epNumber, err)
		}
	}
	return nil
}
