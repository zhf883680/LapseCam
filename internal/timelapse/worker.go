package timelapse

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"timelapse/internal/database"
	"timelapse/internal/ffmpeg"
)

// worker 管理单个任务的抽帧生命周期。
type worker struct {
	taskID int64
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func (s *Service) run(w *worker) {
	defer close(w.done)
	defer func() {
		s.mu.Lock()
		delete(s.workers, w.taskID)
		s.mu.Unlock()
	}()

	task, err := s.getTaskRow(w.taskID)
	if err != nil {
		return
	}
	startedAt := s.ensureStartedAt(task)
	s.captureLoop(w)
	s.finishTask(task, startedAt)
}

// ensureStartedAt 记录任务实际开始时间（首次启动时写入，重启后保留）。
func (s *Service) ensureStartedAt(task Task) time.Time {
	if task.ActualStartedAt != nil {
		return *task.ActualStartedAt
	}
	now := time.Now()
	_, _ = s.db.Exec(`UPDATE timelapse_tasks SET actual_started_at=? WHERE id=? AND actual_started_at IS NULL`,
		now.UTC().Format(time.RFC3339), task.ID)
	return now
}

// captureLoop 持续抽帧；ffmpeg 异常退出时按退避序列自动重连。
func (s *Service) captureLoop(w *worker) {
	task, err := s.getTaskRow(w.taskID)
	if err != nil {
		return
	}
	cam, err := s.cam.Get(task.CameraID)
	if err != nil {
		s.setStatus(w.taskID, StatusFailed, "camera not found: "+err.Error())
		return
	}

	url := ffmpeg.BuildRTSPURL(cam.RtspURL, cam.Username, cam.Password)
	framesDir := s.storage.FramesDir(task.ID)
	if err := s.storage.EnsureDir(framesDir); err != nil {
		s.setStatus(w.taskID, StatusFailed, "create frames dir: "+err.Error())
		return
	}

	logFile, err := s.openTaskLog(task.ID)
	if err != nil {
		s.setStatus(w.taskID, StatusFailed, "open log: "+err.Error())
		return
	}
	defer logFile.Close()

	backoffIdx := 0
	for {
		if w.ctx.Err() != nil {
			return // 停止请求
		}
		if s.taskShouldEnd(task.ID) {
			return // 定时结束
		}

		count, _ := s.storage.CountFiles(framesDir)
		startNumber := count // 覆盖可能不完整的最后一帧，避免编号空洞
		if startNumber == 0 {
			startNumber = 1
		}

		opts := ffmpeg.CaptureOptions{
			IntervalSeconds: float64(task.IntervalSeconds),
			JPEGQuality:     s.cfg.FFmpeg.CaptureJPEGQuality,
			RTSPTransport:   s.cfg.FFmpeg.RTSPTransport,
		}
		sessionStart := time.Now()
		proc, err := s.ff.StartCapture(w.ctx, url, filepath.Join(framesDir, "%06d.jpg"), startNumber, opts, logFile)
		if err != nil {
			s.cam.MarkActivity(cam.ID, false, time.Now())
			backoffIdx = s.waitBackoff(w, backoffIdx)
			continue
		}

		select {
		case <-w.ctx.Done():
			// 优雅停止：SIGTERM → 最多等 10s → SIGKILL
			_ = proc.Signal(syscall.SIGTERM)
			select {
			case <-proc.Done():
			case <-time.After(10 * time.Second):
				_ = proc.Kill()
				<-proc.Done()
			}
			return

		case <-proc.Done():
			if w.ctx.Err() != nil {
				return
			}
			// 进程非预期退出：RTSP 断线等情况 → 退避重连
			s.cam.MarkActivity(cam.ID, false, time.Now())
			if err := proc.Err(); err != nil {
				fmt.Fprintf(logFile, "[%s] capture exited: %v\n", time.Now().Format(time.RFC3339), err)
			}
			// 本次会话持续较久说明网络稳定，重置退避
			if time.Since(sessionStart) > 60*time.Second {
				backoffIdx = 0
			}
			backoffIdx = s.waitBackoff(w, backoffIdx)
		}
	}
}

// waitBackoff 按 5s/10s/30s/60s 序列等待后重连，期间响应停止请求。
func (s *Service) waitBackoff(w *worker, idx int) int {
	backoffs := s.cfg.FFmpeg.CaptureBackoff
	if len(backoffs) == 0 {
		backoffs = []time.Duration{5 * time.Second}
	}
	if idx >= len(backoffs) {
		idx = len(backoffs) - 1
	}
	d := backoffs[idx]
	select {
	case <-w.ctx.Done():
	case <-time.After(d):
	}
	return idx + 1
}

// taskShouldEnd 检查任务是否已到结束时间。
func (s *Service) taskShouldEnd(id int64) bool {
	task, err := s.getTaskRow(id)
	if err != nil {
		return true
	}
	if task.EndAt != nil && !time.Now().UTC().Before(task.EndAt.UTC()) {
		return true
	}
	return false
}

// finishInterrupted 容器重启时，为上次处于 stopping 的任务完成编码收尾。
func (s *Service) finishInterrupted(task Task) {
	startedAt := time.Now()
	if task.ActualStartedAt != nil {
		startedAt = *task.ActualStartedAt
	}
	s.finishTask(task, startedAt)
}

// finishTask 抽帧结束后：生成 MP4 → 写入视频记录 → 更新任务状态。
func (s *Service) finishTask(task Task, startedAt time.Time) {
	// captureLoop 已经失败（如摄像头被删）时直接收尾，不重复覆盖错误信息
	if cur, err := s.getTaskRow(task.ID); err == nil && cur.Status == StatusFailed {
		return
	}
	framesDir := s.storage.FramesDir(task.ID)
	count, _ := s.storage.CountFiles(framesDir)
	if count == 0 {
		msg := "no frames captured"
		s.setStatus(task.ID, StatusFailed, msg)
		s.createRecord(task, startedAt, time.Now(), 0, 0, "", RecordFailed, msg)
		return
	}

	videosDir := s.storage.VideosDir(task.ID)
	if err := s.storage.EnsureDir(videosDir); err != nil {
		s.setStatus(task.ID, StatusFailed, "create videos dir: "+err.Error())
		return
	}

	output := s.videoOutputPath(task)
	logFile, err := s.openTaskLog(task.ID)
	if err == nil {
		defer logFile.Close()
	}
	var stderr *os.File
	if logFile != nil {
		stderr = logFile
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	err = s.ff.Encode(ctx, filepath.Join(framesDir, "%06d.jpg"), output,
		task.OutputFPS, task.Width, task.Height,
		s.cfg.FFmpeg.EncodePreset, s.cfg.FFmpeg.EncodeCRF, stderr)
	if err != nil {
		s.setStatus(task.ID, StatusFailed, "encode: "+err.Error())
		s.createRecord(task, startedAt, time.Now(), count, 0, "", RecordFailed, err.Error())
		return
	}

	info, err := os.Stat(output)
	if err != nil {
		s.setStatus(task.ID, StatusFailed, "stat video: "+err.Error())
		s.createRecord(task, startedAt, time.Now(), count, 0, "", RecordFailed, err.Error())
		return
	}

	s.setStatus(task.ID, StatusCompleted, "")
	s.createRecord(task, startedAt, time.Now(), count, info.Size(), output, RecordSuccess, "")
}

// videoOutputPath 生成形如：videos/task-1/植物生长_2026-08-12.mp4
func (s *Service) videoOutputPath(task Task) string {
	dir := s.storage.VideosDir(task.ID)
	name := s.storage.SanitizeFilename(task.Name)
	date := task.StartAt.Local().Format("2006-01-02")
	base := fmt.Sprintf("%s_%s", name, date)
	path := filepath.Join(dir, base+".mp4")
	for i := 2; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
		path = filepath.Join(dir, fmt.Sprintf("%s_%d.mp4", base, i))
	}
}

func (s *Service) openTaskLog(taskID int64) (*os.File, error) {
	dir := filepath.Join(s.cfg.Storage.BaseDir, "logs")
	if err := s.storage.EnsureDir(dir); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, fmt.Sprintf("task-%d.log", taskID)), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

func (s *Service) createRecord(task Task, start, end time.Time, frames int, size int64, path, status, errMsg string) {
	_, _ = s.db.Exec(`INSERT INTO timelapse_records
		(task_id, start_time, end_time, frame_count, duration_seconds, file_path, file_size, status, error_message, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID,
		start.UTC().Format(time.RFC3339),
		end.UTC().Format(time.RFC3339),
		frames,
		float64(frames)/float64(task.OutputFPS),
		path,
		size,
		status,
		errMsg,
		database.NowUTC(),
		database.NowUTC())
}
