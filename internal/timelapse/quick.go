package timelapse

import (
	"database/sql"
	"errors"
	"sync"
	"time"
)

// quickMu 串行化快捷录制 start/stop，避免并发创建两个任务或状态竞争。
// 生产环境单服务实例，包级锁等价于实例级锁；放在这里避免改动 Service 结构体。
var quickMu sync.Mutex

// ErrNoCamera 没有可用摄像头时返回。
var ErrNoCamera = errors.New("没有可用摄像头")

// QuickStart 开始快捷录制：使用第一台摄像头立即创建录制任务。
// 第二个返回值标记是否此前已在录制（幂等）。
func (s *Service) QuickStart() (Task, bool, error) {
	quickMu.Lock()
	defer quickMu.Unlock()

	if t, err := s.findActiveQuickTask(s.cfg.Quick.Name); err != nil {
		return Task{}, false, err
	} else if t != nil {
		return *t, true, nil
	}

	cams, err := s.cam.List()
	if err != nil {
		return Task{}, false, err
	}
	if len(cams) == 0 {
		return Task{}, false, ErrNoCamera
	}

	in := TaskInput{
		Name:            s.cfg.Quick.Name,
		CameraID:        cams[0].ID,
		IntervalSeconds: s.cfg.Quick.IntervalSeconds,
		OutputFPS:       s.cfg.Quick.OutputFPS,
		Width:           s.cfg.Quick.Width,
		Height:          s.cfg.Quick.Height,
		StartAt:         time.Now().Format(time.RFC3339),
	}
	t, err := s.Create(in)
	if err != nil {
		return Task{}, false, err
	}
	return t, false, nil
}

// QuickStop 停止当前正在录制的快捷任务并出片。
// 没有正在录制的快捷任务时返回 found=false（幂等）。
func (s *Service) QuickStop() (Task, bool, error) {
	quickMu.Lock()
	defer quickMu.Unlock()

	t, err := s.findActiveQuickTask(s.cfg.Quick.Name)
	if err != nil {
		return Task{}, false, err
	}
	if t == nil {
		return Task{}, false, nil
	}
	if err := s.Stop(t.ID); err != nil {
		return Task{}, false, err
	}
	cur, err := s.Get(t.ID)
	if err != nil {
		return Task{}, false, err
	}
	return cur, true, nil
}

// findActiveQuickTask 查找 name 指定的 running/stopping 任务，没有则返回 nil。
func (s *Service) findActiveQuickTask(name string) (*Task, error) {
	row := s.db.QueryRow(`SELECT id, name, camera_id, interval_seconds, output_fps, width, height,
		start_at, end_at, status, error_message, actual_started_at, created_at, updated_at
		FROM timelapse_tasks WHERE name=? AND status IN (?, ?) ORDER BY id DESC LIMIT 1`,
		name, StatusRunning, StatusStopping)
	var t Task
	err := scanTask(row, &t)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}
