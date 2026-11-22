package handlers

import (
	"errors"
	"fmt"
	"log"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/models"
	"fan-web/services"
	"fan-web/services/matching"
	"fan-web/utils"
)

// 辅助匹配业务结论（AssistMatchResponse.Status）。
const (
	assistStatusCommitted      = "committed"
	assistStatusMatched        = "matched"
	assistStatusReviewRequired = "review_required"
	assistStatusNoMatch        = "no_match"
)

// AssistMatch POST /api/library/assist-match：外部工具（Hermes）下载完成后的辅助匹配入库。
// 协议统一原则：匹配业务结论（committed/matched/review_required/no_match）一律 utils.Success +
// AssistMatchResponse；仅参数非法（1001）、引擎不支持、跨目录绑定、他番/多番认领或 CAS 冲突、
// 上游故障走 confirmFail。建番/写剧集全部复用 commitInboxFiles（confirmLock + prepareScanV2 + commitScanV2）。
func (h *AnimeHandler) AssistMatch(c *gin.Context) {
	var request models.AssistMatchRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		confirmFail(c, utils.CodeInvalidParams, confirmReasonInvalid, "请求参数错误", nil)
		return
	}
	autoCommit := request.AutoCommit == nil || *request.AutoCommit
	if request.BangumiID < 0 {
		confirmFail(c, utils.CodeInvalidParams, confirmReasonInvalid, "bangumi_id 不能为负数", nil)
		return
	}
	if request.SeasonHint < 0 {
		confirmFail(c, utils.CodeInvalidParams, confirmReasonInvalid, "season_hint 不能为负数", nil)
		return
	}
	if request.EpisodeOffset < 0 {
		confirmFail(c, utils.CodeInvalidParams, confirmReasonInvalid, "episode_offset 不能为负数", nil)
		return
	}
	if err := services.ValidateRelativeVideoPath(request.FilePath); err != nil {
		confirmFail(c, utils.CodeInvalidParams, confirmReasonInvalid, err.Error(), nil)
		return
	}
	for _, name := range request.FileNames {
		if !validateConfirmFileName(name) {
			confirmFail(c, utils.CodeInvalidParams, confirmReasonInvalid, fmt.Sprintf("文件名不合法: %q", name), nil)
			return
		}
	}
	// 解析/建档之前判断引擎：legacy / 无 MatchingRuntime 不支持辅助匹配。
	if h.scanner.MatchEngine() != matching.EngineV2 {
		confirmFail(c, utils.CodeInternal, confirmReasonUnsupportedEngine, "当前匹配引擎不支持辅助匹配", nil)
		return
	}

	// 第 1 步：路径归一化 + 归属检测（空目录 / 全已关联幂等 / 他番冲突）。
	resolved, err := h.scanner.ResolveAssistInputPath(request.FilePath, request.FileNames)
	if err != nil {
		log.Printf("[Assist] 解析辅助匹配路径 %q 失败: %v", request.FilePath, err)
		confirmFail(c, utils.CodeInvalidParams, confirmReasonInvalid, err.Error(), nil)
		return
	}
	normDir := resolved.RelDir
	if len(resolved.PresentFiles) == 0 {
		confirmFail(c, utils.CodeInvalidParams, confirmReasonInvalid, "目录下没有可识别的视频文件", nil)
		return
	}

	targets := resolved.FreeFiles
	explicit := len(request.FileNames) > 0 || resolved.IsSingleFilePath
	// 显式文件中已归属的番剧（仅显式模式）：title_hint 路径解析出其他条目时须与 bangumi_id 路径同样判冲突。
	var explicitOwned *models.Anime
	var explicitOwnedFiles []string
	switch {
	case explicit:
		owned, err := h.scanner.InspectAssistFilesOwnership(normDir, resolved.FreeFiles)
		if err != nil {
			log.Printf("[Assist] 检查目录 %q 文件归属失败: %v", normDir, err)
			confirmFail(c, utils.CodeInternal, confirmReasonUpstream, "检查文件归属失败", nil)
			return
		}
		if len(owned.ConflictFiles) > 0 {
			confirmFail(c, utils.CodeInternal, confirmReasonConflict, assistConflictMessage(owned.ConflictReason), owned.ConflictFiles)
			return
		}
		if request.BangumiID > 0 && owned.OwnedAnime != nil && owned.OwnedAnime.BangumiID != request.BangumiID {
			confirmFail(c, utils.CodeInternal, confirmReasonConflict, "文件已归属其他番剧", assistOwnedFileNames(resolved.FreeFiles, owned.Unassociated))
			return
		}
		if len(owned.Unassociated) == 0 && owned.OwnedAnime != nil {
			h.assistIdempotent(c, normDir, resolved.FreeFiles, owned)
			return
		}
		targets = owned.Unassociated
		if owned.OwnedAnime != nil {
			explicitOwned = owned.OwnedAnime
			explicitOwnedFiles = assistOwnedFileNames(resolved.FreeFiles, owned.Unassociated)
		}
	case len(resolved.FreeFiles) == 0:
		// 目录模式且全部文件已关联：仅 1 部番剧认领全部文件且与请求目标一致时幂等返回，否则视为冲突。
		owned, err := h.scanner.InspectAssistFilesOwnership(normDir, resolved.PresentFiles)
		if err != nil {
			log.Printf("[Assist] 检查目录 %q 文件归属失败: %v", normDir, err)
			confirmFail(c, utils.CodeInternal, confirmReasonUpstream, "检查文件归属失败", nil)
			return
		}
		if owned.OwnedAnime != nil && len(owned.ConflictFiles) == 0 && len(owned.Unassociated) == 0 &&
			(request.BangumiID <= 0 || owned.OwnedAnime.BangumiID == request.BangumiID) {
			h.assistIdempotent(c, normDir, resolved.PresentFiles, owned)
			return
		}
		conflictFiles := owned.ConflictFiles
		if len(conflictFiles) == 0 {
			conflictFiles = resolved.PresentFiles
		}
		confirmFail(c, utils.CodeInternal, confirmReasonConflict, assistConflictMessage(owned.ConflictReason), conflictFiles)
		return
	}

	// 第 2 步：分流解析目标 BangumiID。
	if request.BangumiID > 0 {
		if !autoCommit {
			result, err := h.scanner.PreviewAssistByBangumiID(c.Request.Context(), normDir, targets, request.BangumiID, request.EpisodeOffset)
			if err != nil {
				assistServiceFail(c, err)
				return
			}
			if result.Decision.Code == matching.Accepted {
				utils.Success(c, models.AssistMatchResponse{
					Status:    assistStatusMatched,
					Candidate: result.Candidate,
					Bindings:  result.Bindings,
					FilePath:  normDir,
					FileNames: targets,
				})
				return
			}
			utils.Success(c, models.AssistMatchResponse{
				Status:     assistStatusReviewRequired,
				Reason:     assistReviewReason(result.Decision),
				Candidates: result.Candidates,
				FilePath:   normDir,
				FileNames:  targets,
			})
			return
		}
		h.assistCommit(c, request.BangumiID, normDir, targets, request.EpisodeOffset, nil)
		return
	}

	result, err := h.scanner.ResolveAssistByHint(c.Request.Context(), services.AssistHintInput{
		RelDir:        normDir,
		TitleHint:     request.TitleHint,
		FileNames:     targets,
		SeasonHint:    request.SeasonHint,
		EpisodeOffset: request.EpisodeOffset,
	})
	if err != nil {
		assistServiceFail(c, err)
		return
	}
	if result.Decision.Code == matching.Accepted && result.Subject != nil {
		if explicitOwned != nil && explicitOwned.BangumiID != result.Subject.ID {
			confirmFail(c, utils.CodeInternal, confirmReasonConflict, "文件已归属其他番剧", explicitOwnedFiles)
			return
		}
		if !autoCommit {
			utils.Success(c, models.AssistMatchResponse{
				Status:    assistStatusMatched,
				Candidate: result.Candidate,
				Bindings:  result.Bindings,
				FilePath:  normDir,
				FileNames: targets,
			})
			return
		}
		// 提交沿用本次预览的章节绑定（含季锚点映射），避免受信任重解释丢失锚点而与预览结论不一致。
		h.assistCommit(c, result.Subject.ID, normDir, targets, request.EpisodeOffset, result.Chapters.Bindings)
		return
	}

	status := assistStatusReviewRequired
	if result.Decision.Code == matching.NoCandidates {
		status = assistStatusNoMatch
	}
	reason := assistReviewReason(result.Decision)
	if status == assistStatusNoMatch && result.Decision.Reason == "" {
		reason = "未找到匹配条目"
	}
	if autoCommit {
		// 复核态落收件箱：targets 已在第 1 步过滤掉已关联文件，只影响本次文件，不覆盖其它条目。
		rows := make([]models.UnidentifiedFile, 0, len(targets))
		for _, name := range targets {
			rows = append(rows, models.UnidentifiedFile{
				FileName:   name,
				Reason:     reason,
				FilePath:   normDir,
				Candidates: result.Candidates,
			})
		}
		if err := database.UpsertUnidentifiedFiles(rows); err != nil {
			log.Printf("[Assist] 写入目录 %q 收件箱失败: %v", normDir, err)
			confirmFail(c, utils.CodeInternal, confirmReasonUpstream, "写入收件箱失败", nil)
			return
		}
	}
	utils.Success(c, models.AssistMatchResponse{
		Status:     status,
		Reason:     reason,
		Candidates: result.Candidates,
		FilePath:   normDir,
		FileNames:  targets,
	})
}

// assistCommit 第 3 步：经共用 commitInboxFiles（受信任目标）原子入库。
// 章节不可解释（review_required）或文件未能全部归入（no_match）属于匹配业务结论：
// commitInboxFiles 已先回滚本请求新建的空番剧，再以 utils.Success 返回；其余失败走 confirmFail。
// bindings 非空时为同一次请求预览得出的章节绑定（title_hint 路径），提交直接沿用；nil 时按目标章节受信任解释。
func (h *AnimeHandler) assistCommit(c *gin.Context, bangumiID int, normDir string, fileNames []string, episodeOffset int, bindings []matching.EpisodeBinding) {
	result, fail := h.commitInboxFiles(inboxCommitRequest{
		BangumiID:       bangumiID,
		Dir:             normDir,
		FileNames:       fileNames,
		EpisodeOffset:   episodeOffset,
		TrustedTarget:   true,
		TrustedBindings: bindings,
	})
	if fail != nil {
		switch fail.reason {
		case confirmReasonReviewRequired:
			utils.Success(c, models.AssistMatchResponse{
				Status:    assistStatusReviewRequired,
				Reason:    fail.message,
				FilePath:  normDir,
				FileNames: fileNames,
			})
		case confirmReasonNoMatch:
			utils.Success(c, models.AssistMatchResponse{
				Status:    assistStatusNoMatch,
				Reason:    fail.message,
				FilePath:  normDir,
				FileNames: fail.fileNames,
			})
		default:
			confirmFail(c, fail.code, fail.reason, fail.message, fail.fileNames)
		}
		return
	}
	utils.Success(c, models.AssistMatchResponse{
		Status:    assistStatusCommitted,
		Anime:     result.anime,
		Created:   result.created,
		Scanned:   len(result.episodes),
		Episodes:  result.episodes,
		FilePath:  normDir,
		FileNames: fileNames,
	})
}

// assistIdempotent 目标文件已全部归属同一番剧：幂等返回 committed, created=false，不写库。
func (h *AnimeHandler) assistIdempotent(c *gin.Context, normDir string, fileNames []string, owned services.AssistOwnershipStatus) {
	utils.Success(c, models.AssistMatchResponse{
		Status:    assistStatusCommitted,
		Anime:     owned.OwnedAnime,
		Created:   false,
		Scanned:   len(owned.OwnedEpisodes),
		Episodes:  owned.OwnedEpisodes,
		FilePath:  normDir,
		FileNames: fileNames,
	})
}

// assistServiceFail 辅助匹配服务层 error：参数类 → 1001；其余（上游网络/限流等）→ 9999 upstream。
func assistServiceFail(c *gin.Context, err error) {
	if errors.Is(err, services.ErrAssistInvalidInput) || errors.Is(err, services.ErrInvalidVideoPath) {
		confirmFail(c, utils.CodeInvalidParams, confirmReasonInvalid, err.Error(), nil)
		return
	}
	log.Printf("[Assist] 辅助匹配上游失败: %v", err)
	confirmFail(c, utils.CodeInternal, confirmReasonUpstream, "匹配上游不可用，未写入任何变更", nil)
}

// assistReviewReason 复核态原因：优先使用 Decision.Reason。
func assistReviewReason(d matching.Decision) string {
	if d.Reason != "" {
		return d.Reason
	}
	return "匹配需要人工确认"
}

func assistConflictMessage(reason string) string {
	if reason != "" {
		return reason
	}
	return "文件已归属其他番剧"
}

// assistOwnedFileNames 返回 all 中不在 unassociated 里的文件名（即已被认领的文件）。
func assistOwnedFileNames(all, unassociated []string) []string {
	free := make(map[string]bool, len(unassociated))
	for _, n := range unassociated {
		free[n] = true
	}
	out := make([]string, 0, len(all))
	for _, n := range all {
		if !free[n] {
			out = append(out, n)
		}
	}
	return out
}
