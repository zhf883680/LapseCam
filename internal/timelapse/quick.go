package timelapse

import (
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
