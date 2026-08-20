package services

import (
	"fmt"
	"sync"
	"time"

	"fan-web/database"
	"fan-web/models"
)

const (
	ScanJobIdle    = "idle"
	ScanJobRunning = "running"
	ScanJobDone    = "done"
	ScanJobError   = "error"
)

// ScanJob 是全库扫描作业的 JSON 快照。
type ScanJob struct {
	State      string             `json:"state"`
	StartedAt  *time.Time         `json:"started_at,omitempty"`
	FinishedAt *time.Time         `json:"finished_at,omitempty"`
	Error      string             `json:"error,omitempty"`
	Result     *LibraryScanResult `json:"result,omitempty"`
}

// LibraryJob 进程内单槽扫描作业。同一时刻只跑一趟 Scan。
type LibraryJob struct {
	lib     *LibraryService
	mu      sync.Mutex
	current ScanJob
}

func NewLibraryJob(lib *LibraryService) *LibraryJob {
	return &LibraryJob{
		lib:     lib,
		current: ScanJob{State: ScanJobIdle},
	}
}

func (j *LibraryJob) Snapshot() ScanJob {
	j.mu.Lock()
	defer j.mu.Unlock()
	return copyScanJob(j.current)
}

// Start 在 idle/done/error 时开 goroutine 跑 Scan 并立即返回 running。
// 已 running 则返回当前快照，不启第二趟。
func (j *LibraryJob) Start() ScanJob {
	j.mu.Lock()
	if j.current.State == ScanJobRunning {
		snap := copyScanJob(j.current)
		j.mu.Unlock()
		return snap
	}
	now := time.Now().UTC()
	j.current = ScanJob{State: ScanJobRunning, StartedAt: &now}
	snap := copyScanJob(j.current)
	j.mu.Unlock()
	go j.run()
	return snap
}

func (j *LibraryJob) run() {
	defer func() {
		if rec := recover(); rec != nil {
			j.fail(fmt.Sprint(rec))
		}
	}()

	result, err := j.lib.Scan()
	if err != nil {
		j.fail(err.Error())
		return
	}

	rows, err := unidentifiedRowsForWrite(result.Unidentified)
	if err != nil {
		j.fail(err.Error())
		return
	}
	if err := database.ReplaceUnidentified(rows); err != nil {
		j.fail(err.Error())
		return
	}
	j.succeed(result)
}

func (j *LibraryJob) fail(message string) {
	now := time.Now().UTC()
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.current.State != ScanJobRunning {
		return
	}
	j.current.State = ScanJobError
	j.current.FinishedAt = &now
	j.current.Error = message
	j.current.Result = nil
}

func (j *LibraryJob) succeed(result *LibraryScanResult) {
	now := time.Now().UTC()
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.current.State != ScanJobRunning {
		return
	}
	j.current.State = ScanJobDone
	j.current.FinishedAt = &now
	j.current.Error = ""
	j.current.Result = result
}

func unidentifiedRowsForWrite(files []UnidentifiedFile) ([]models.UnidentifiedFile, error) {
	rows := make([]models.UnidentifiedFile, 0, len(files))
	for _, file := range files {
		associated, err := database.IsFileAssociated(file.FileName, file.FilePath)
		if err != nil {
			return nil, err
		}
		if associated {
			continue
		}
		candidates := make([]models.MatchCandidate, 0, len(file.Candidates))
		for _, candidate := range file.Candidates {
			candidates = append(candidates, models.MatchCandidate{
				ID:     candidate.ID,
				Name:   candidate.Name,
				NameCn: candidate.NameCn,
				Score:  candidate.Score,
			})
		}
		rows = append(rows, models.UnidentifiedFile{
			FileName:   file.FileName,
			Reason:     file.Reason,
			FilePath:   file.FilePath,
			Candidates: candidates,
		})
	}
	return rows, nil
}

func copyScanJob(job ScanJob) ScanJob {
	out := job
	if job.StartedAt != nil {
		started := *job.StartedAt
		out.StartedAt = &started
	}
	if job.FinishedAt != nil {
		finished := *job.FinishedAt
		out.FinishedAt = &finished
	}
	return out
}
