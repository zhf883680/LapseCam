package timelapse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"timelapse/config"
	"timelapse/internal/camera"
	"timelapse/internal/database"
	"timelapse/internal/ffmpeg"
	"timelapse/internal/storage"
)

var (
	ErrNotFound = errors.New("timelapse task not found")
)

// Service 负责任务 CRUD、调度与 worker 生命周期管理。
type Service struct {
	db      *sql.DB
	cfg     *config.Config
	ff      *ffmpeg.Service
	cam     *camera.Service
	storage *storage.Service

	mu      sync.Mutex
	workers map[int64]*worker
}

func New(db *sql.DB, cfg *config.Config, ff *ffmpeg.Service, cam *camera.Service, st *storage.Service) *Service {
	return &Service{
		db:      db,
		cfg:     cfg,
		ff:      ff,
		cam:     cam,
		storage: st,
		workers: make(map[int64]*worker),
	}
}

// StartScheduler 启动调度器，并恢复上次未完成的任务（容器重启场景）。
func (s *Service) StartScheduler(ctx context.Context) {
	s.resumeRunning()
	go s.scheduleLoop(ctx)
}

// Create 创建任务；如果开始时间已到则立即启动。
func (s *Service) Create(in TaskInput) (Task, error) {
	startAt, endAt, err := parseTimes(in)
	if err != nil {
		return Task{}, err
	}

	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return Task{}, errors.New("name is required")
	}
	if in.CameraID <= 0 {
		return Task{}, errors.New("cameraId is required")
	}
	if _, err := s.cam.Get(in.CameraID); err != nil {
		return Task{}, fmt.Errorf("camera: %w", err)
	}
	if in.IntervalSeconds < 1 {
		return Task{}, errors.New("intervalSeconds must be >= 1")
	}
	if in.OutputFPS < 1 || in.OutputFPS > 120 {
		return Task{}, errors.New("outputFps must be 1-120")
	}
	if in.Width <= 0 || in.Height <= 0 {
		return Task{}, errors.New("width and height must be positive")
	}
	if endAt != nil && !endAt.After(startAt) {
		return Task{}, errors.New("endAt must be after startAt")
	}

	now := database.NowUTC()
	startStr := startAt.UTC().Format(time.RFC3339)
	var endStr any
	if endAt != nil {
		endStr = endAt.UTC().Format(time.RFC3339)
	}

	res, err := s.db.Exec(`INSERT INTO timelapse_tasks
		(name, camera_id, interval_seconds, output_fps, width, height, start_at, end_at, status, error_message, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?)`,
		in.Name, in.CameraID, in.IntervalSeconds, in.OutputFPS, in.Width, in.Height,
		startStr, endStr, StatusPending, now, now)
	if err != nil {
		return Task{}, err
	}
	id, _ := res.LastInsertId()

	// 开始时间已到（或已过）：立即进入运行状态
	if !startAt.After(time.Now()) {
		if err := s.Start(id); err != nil {
			return Task{}, err
		}
	}
	return s.Get(id)
}

// Get 返回任务详情（含摄像头名、帧数、进度）。
func (s *Service) Get(id int64) (Task, error) {
	t, err := s.getTaskRow(id)
	if err != nil {
		return Task{}, err
	}
	s.enrich(&t)
	return t, nil
}

// List 返回任务列表（按创建时间倒序）。
func (s *Service) List() ([]Task, error) {
	rows, err := s.db.Query(`SELECT id, name, camera_id, interval_seconds, output_fps, width, height, start_at, end_at, status, error_message, actual_started_at, created_at, updated_at
		FROM timelapse_tasks ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Task, 0)
	for rows.Next() {
		var t Task
		if err := scanTask(rows, &t); err != nil {
			return nil, err
		}
		s.enrich(&t)
		out = append(out, t)
	}
	return out, rows.Err()
}

// Start 手动启动一个未运行的任务。
func (s *Service) Start(id int64) error {
	t, err := s.getTaskRow(id)
	if err != nil {
		return err
	}
	switch t.Status {
	case StatusRunning, StatusStopping:
		return errors.New("task is already running")
	case StatusCompleted, StatusFailed:
		return errors.New("task already finished, create a new one")
	}

	// 重新开始前清空旧帧
	if err := s.storage.RemoveDir(s.storage.FramesDir(id)); err != nil {
		return err
	}
	return s.startWorker(id)
}

// Stop 请求停止运行中的任务（异步：停止抽帧 → 生成视频 → 更新状态）。
func (s *Service) Stop(id int64) error {
	s.mu.Lock()
	w, ok := s.workers[id]
	s.mu.Unlock()

	t, err := s.getTaskRow(id)
	if err != nil {
		return err
	}

	switch t.Status {
	case StatusPending:
		// 还没开始：直接标记为 stopped（无视频）
		if err := s.setStatus(id, StatusStopped, ""); err != nil {
			return err
		}
		return nil
	case StatusRunning:
		if !ok {
			return errors.New("task has no worker, please retry")
		}
		if err := s.setStatus(id, StatusStopping, ""); err != nil {
			return err
		}
		w.cancel()
		return nil
	default:
		return fmt.Errorf("task status is %s, cannot stop", t.Status)
	}
}

// Delete 删除任务：停止 worker → 删除数据库记录与 frames/videos 文件。
func (s *Service) Delete(id int64) error {
	s.mu.Lock()
	w, ok := s.workers[id]
	s.mu.Unlock()

	if ok {
		w.cancel()
		select {
		case <-w.done:
		case <-time.After(10 * time.Minute):
			return errors.New("task is still finishing, retry later")
		}
	}

	// 删除关联视频记录及其文件
	records, err := s.listRecordsByTask(id)
	if err != nil {
		return err
	}
	for _, r := range records {
		if r.FilePath != "" {
			_ = s.storage.RemoveFile(r.FilePath)
		}
	}
	if _, err := s.db.Exec(`DELETE FROM timelapse_records WHERE task_id=?`, id); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM timelapse_tasks WHERE id=?`, id); err != nil {
		return err
	}
	_ = s.storage.RemoveDir(s.storage.FramesDir(id))
	_ = s.storage.RemoveDir(s.storage.VideosDir(id))
	return nil
}

// HasActiveTasks 检查摄像头是否被未结束的任务引用（用于删除摄像头前的校验）。
func (s *Service) HasActiveTasks(cameraID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM timelapse_tasks
		WHERE camera_id=? AND status IN (?, ?, ?)`,
		cameraID, StatusPending, StatusRunning, StatusStopping).Scan(&n)
	return n > 0, err
}

// ---- 调度 ----

func (s *Service) scheduleLoop(ctx context.Context) {
	tick := time.Duration(s.cfg.Scheduler.TickSeconds) * time.Second
	if tick <= 0 {
		tick = time.Second
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processScheduled()
		}
	}
}

func (s *Service) processScheduled() {
	now := database.NowUTC()

	// 到期自动开始
	rows, err := s.db.Query(`SELECT id FROM timelapse_tasks WHERE status=? AND start_at <= ?`, StatusPending, now)
	if err == nil {
		var ids []int64
		for rows.Next() {
			var id int64
			_ = rows.Scan(&id)
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			_ = s.Start(id)
		}
	}

	// 到期自动停止
	rows, err = s.db.Query(`SELECT id FROM timelapse_tasks WHERE status=? AND end_at IS NOT NULL AND end_at <= ?`, StatusRunning, now)
	if err == nil {
		var ids []int64
		for rows.Next() {
			var id int64
			_ = rows.Scan(&id)
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			_ = s.Stop(id)
		}
	}
}

// resumeRunning 容器重启后：
//   - running 的任务恢复为运行态继续抽帧（编号自动接续）
//   - stopping 的任务不继续抽帧，直接完成编码收尾
func (s *Service) resumeRunning() {
	rows, err := s.db.Query(`SELECT id, status FROM timelapse_tasks WHERE status IN (?, ?)`, StatusRunning, StatusStopping)
	if err != nil {
		return
	}
	type rowT struct {
		id     int64
		status string
	}
	var rows2 []rowT
	for rows.Next() {
		var id int64
		var status string
		_ = rows.Scan(&id, &status)
		rows2 = append(rows2, rowT{id: id, status: status})
	}
	rows.Close()
	for _, r := range rows2 {
		if r.status == StatusStopping {
			t, err := s.getTaskRow(r.id)
			if err != nil {
				continue
			}
			go s.finishInterrupted(t)
			continue
		}
		_ = s.startWorker(r.id)
	}
}

// ---- worker 管理 ----

func (s *Service) startWorker(id int64) error {
	s.mu.Lock()
	if _, ok := s.workers[id]; ok {
		s.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &worker{taskID: id, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	s.workers[id] = w
	s.mu.Unlock()

	if err := s.setStatus(id, StatusRunning, ""); err != nil {
		s.mu.Lock()
		delete(s.workers, id)
		s.mu.Unlock()
		cancel()
		return err
	}
	go s.run(w)
	return nil
}

func (s *Service) setStatus(id int64, status, errMsg string) error {
	_, err := s.db.Exec(`UPDATE timelapse_tasks SET status=?, error_message=?, updated_at=? WHERE id=?`,
		status, errMsg, database.NowUTC(), id)
	return err
}

func (s *Service) getTaskRow(id int64) (Task, error) {
	row := s.db.QueryRow(`SELECT id, name, camera_id, interval_seconds, output_fps, width, height, start_at, end_at, status, error_message, actual_started_at, created_at, updated_at
		FROM timelapse_tasks WHERE id=?`, id)
	var t Task
	err := scanTask(row, &t)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	return t, err
}

func scanTask(r interface{ Scan(...any) error }, t *Task) error {
	var startAt, createdAt, updatedAt string
	var endAt, actualStartedAt sql.NullString
	if err := r.Scan(&t.ID, &t.Name, &t.CameraID, &t.IntervalSeconds, &t.OutputFPS, &t.Width, &t.Height,
		&startAt, &endAt, &t.Status, &t.ErrorMessage, &actualStartedAt, &createdAt, &updatedAt); err != nil {
		return err
	}
	t.StartAt = database.ParseTime(startAt)
	if endAt.Valid && endAt.String != "" {
		v := database.ParseTime(endAt.String)
		t.EndAt = &v
	}
	if actualStartedAt.Valid && actualStartedAt.String != "" {
		v := database.ParseTime(actualStartedAt.String)
		t.ActualStartedAt = &v
	}
	t.CreatedAt = database.ParseTime(createdAt)
	t.UpdatedAt = database.ParseTime(updatedAt)
	return nil
}

// enrich 补充摄像头名与实时帧数/进度。
func (s *Service) enrich(t *Task) {
	if t.CameraID > 0 {
		if c, err := s.cam.Get(t.CameraID); err == nil {
			t.CameraName = c.Name
		}
	}
	if count, err := s.storage.CountFiles(s.storage.FramesDir(t.ID)); err == nil {
		t.FrameCount = count
	}
	if (t.Status == StatusRunning || t.Status == StatusStopping) && t.EndAt != nil {
		base := t.StartAt
		if t.ActualStartedAt != nil {
			base = *t.ActualStartedAt
		}
		if t.EndAt.After(base) {
			total := t.EndAt.Sub(base).Seconds()
			if total > 0 {
				elapsed := time.Since(base).Seconds()
				if elapsed < 0 {
					elapsed = 0
				}
				pct := elapsed / total * 100
				if pct > 100 {
					pct = 100
				}
				t.ProgressPct = pct
			}
		}
	}
}

// parseTimes 解析任务的开始/结束时间，兼容多种输入格式；无时区按服务器本地时区解释。
func parseTimes(in TaskInput) (time.Time, *time.Time, error) {
	startAt, err := parseTime(in.StartAt)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("invalid startAt: %w", err)
	}
	var endAt *time.Time
	if in.EndAt != nil && strings.TrimSpace(*in.EndAt) != "" {
		v, err := parseTime(*in.EndAt)
		if err != nil {
			return time.Time{}, nil, fmt.Errorf("invalid endAt: %w", err)
		}
		endAt = &v
	}
	return startAt, endAt, nil
}

func parseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errors.New("empty time")
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse %q", s)
}
