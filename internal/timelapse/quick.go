package timelapse

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"timelapse/config"
	"timelapse/internal/ffmpeg"
)

// quickMu 串行化快捷录制 start/stop，避免并发创建两个任务或状态竞争。
// 生产环境单服务实例，包级锁等价于实例级锁；放在这里避免改动 Service 结构体。
var quickMu sync.Mutex

// quickSnapshotMu 串行化逐层截图（抓帧 + 层去重），避免并发写帧号冲突。
var quickSnapshotMu sync.Mutex

// layerMarkerMu 保护层标记文件的读写。
var layerMarkerMu sync.Mutex

// ErrNoCamera 没有可用摄像头时返回。
var ErrNoCamera = errors.New("没有可用摄像头")

// ErrNoQuickTask 没有正在录制的快捷任务时返回。
var ErrNoQuickTask = errors.New("没有正在录制的快捷任务")

// ErrNotLayerMode 当前 quick.captureMode 不是 layer，不能逐层截图。
var ErrNotLayerMode = errors.New("quick.captureMode 不是 layer，无法逐层截图")

// capturedLayers 记录每个快捷任务已截图的层号，用于 HA 传感器抖动防重（内存态，重启即清）。
var capturedLayers = map[int64]map[int]bool{}

// layerMarker 一条层变化记录（timestamp 模式出片选帧用）。
type layerMarker struct {
	Time  time.Time `json:"time"`
	Layer int       `json:"layer"`
}

// SnapshotResult 逐层截图的结果。
type SnapshotResult struct {
	TaskID   int64 `json:"taskId"`
	Layer    int   `json:"layer"`
	Frame    int   `json:"frame"`
	Captured bool  `json:"captured"`
}

// LayerResult 记录层变化的结果。
type LayerResult struct {
	TaskID   int64 `json:"taskId"`
	Layer    int   `json:"layer"`
	Recorded bool  `json:"recorded"`
}

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
	delete(capturedLayers, t.ID)
	if err := s.Stop(t.ID); err != nil {
		return Task{}, false, err
	}
	cur, err := s.Get(t.ID)
	if err != nil {
		return Task{}, false, err
	}
	return cur, true, nil
}

// QuickSnapshot 为当前快捷任务抓取一帧（captureMode=layer 逐层截图模式）。
// layer>0 时按层号幂等：同一层只截一次，防 HA 传感器抖动重复触发。
func (s *Service) QuickSnapshot(layer int) (SnapshotResult, error) {
	quickSnapshotMu.Lock()
	defer quickSnapshotMu.Unlock()

	if s.cfg.Quick.CaptureMode != config.CaptureModeLayer {
		return SnapshotResult{}, ErrNotLayerMode
	}

	t, err := s.findActiveQuickTask(s.cfg.Quick.Name)
	if err != nil {
		return SnapshotResult{}, err
	}
	if t == nil {
		return SnapshotResult{}, ErrNoQuickTask
	}
	if t.Status != StatusRunning {
		return SnapshotResult{}, errors.New("快捷任务正在收尾，无法截图")
	}

	// 同层防重
	if layer > 0 && capturedLayers[t.ID] != nil && capturedLayers[t.ID][layer] {
		return SnapshotResult{TaskID: t.ID, Layer: layer, Captured: false}, nil
	}

	cam, err := s.cam.Get(t.CameraID)
	if err != nil {
		return SnapshotResult{}, err
	}

	framesDir := s.storage.FramesDir(t.ID)
	if err := s.storage.EnsureDir(framesDir); err != nil {
		return SnapshotResult{}, err
	}
	count, _ := s.storage.CountFiles(framesDir)
	next := count + 1
	output := filepath.Join(framesDir, fmt.Sprintf("%06d.jpg", next))

	var stderr *os.File
	if logFile, err := s.openTaskLog(t.ID); err == nil {
		stderr = logFile
		defer logFile.Close()
	}

	url := ffmpeg.BuildRTSPURL(cam.RtspURL, cam.Username, cam.Password)
	opts := ffmpeg.CaptureOptions{
		JPEGQuality:   s.cfg.FFmpeg.CaptureJPEGQuality,
		RTSPTransport: s.cfg.FFmpeg.RTSPTransport,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.ff.GrabFrame(ctx, url, output, opts, stderr); err != nil {
		return SnapshotResult{}, err
	}

	if capturedLayers[t.ID] == nil {
		capturedLayers[t.ID] = map[int]bool{}
	}
	if layer > 0 {
		capturedLayers[t.ID][layer] = true
	}
	return SnapshotResult{TaskID: t.ID, Layer: layer, Frame: next, Captured: true}, nil
}

// QuickRecordLayer 记录当前层的开始/变化时刻（timestamp 模式出片时据此选帧）。
// 记录本身不依赖 captureMode，任何模式下调用都无害。
func (s *Service) QuickRecordLayer(layer int) (LayerResult, error) {
	t, err := s.findActiveQuickTask(s.cfg.Quick.Name)
	if err != nil {
		return LayerResult{}, err
	}
	if t == nil {
		return LayerResult{}, ErrNoQuickTask
	}
	m := layerMarker{Time: time.Now(), Layer: layer}
	if err := appendLayerMarker(s.storage.MarkersFile(t.ID), m); err != nil {
		return LayerResult{}, err
	}
	return LayerResult{TaskID: t.ID, Layer: layer, Recorded: true}, nil
}

// appendLayerMarker 把一条层标记追加写入任务标记文件。
func appendLayerMarker(path string, m layerMarker) error {
	layerMarkerMu.Lock()
	defer layerMarkerMu.Unlock()
	markers, err := loadLayerMarkersFile(path)
	if err != nil {
		return err
	}
	markers = append(markers, m)
	data, err := json.MarshalIndent(markers, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// loadLayerMarkersFile 读取层标记文件；文件不存在时返回空列表。
func loadLayerMarkersFile(path string) ([]layerMarker, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var markers []layerMarker
	if err := json.Unmarshal(data, &markers); err != nil {
		return nil, err
	}
	return markers, nil
}

// selectFramesByMarkers 在 timestamp 模式下，按层标记从帧目录挑选最接近每层的帧。
// 返回选中的帧号（升序）；模式不符或没有标记时返回 nil（调用方回退全部帧）。
func (s *Service) selectFramesByMarkers(task Task) ([]int, error) {
	if s.cfg.Quick.CaptureMode != config.CaptureModeTimestamp || task.Name != s.cfg.Quick.Name {
		return nil, nil
	}
	markers, err := loadLayerMarkersFile(s.storage.MarkersFile(task.ID))
	if err != nil {
		return nil, err
	}
	if len(markers) == 0 {
		return nil, nil
	}

	framesDir := s.storage.FramesDir(task.ID)
	entries, err := os.ReadDir(framesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	type frameTime struct {
		num int
		t   time.Time
	}
	frames := make([]frameTime, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !e.Type().IsRegular() {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		if !strings.EqualFold(ext, ".jpg") {
			continue
		}
		num, err := strconv.Atoi(strings.TrimSuffix(name, ext))
		if err != nil || num <= 0 {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		frames = append(frames, frameTime{num: num, t: info.ModTime()})
	}
	if len(frames) == 0 {
		return nil, nil
	}
	sort.Slice(frames, func(i, j int) bool { return frames[i].t.Before(frames[j].t) })
	sort.Slice(markers, func(i, j int) bool { return markers[i].Time.Before(markers[j].Time) })

	offset := time.Duration(s.cfg.Quick.LayerOffsetSeconds * float64(time.Second))
	window := time.Duration(s.cfg.Quick.LayerWindowSeconds * float64(time.Second))

	picked := make(map[int]bool)
	for _, m := range markers {
		target := m.Time.Add(offset)
		bestNum := 0
		var bestDist time.Duration
		for _, f := range frames {
			d := f.t.Sub(target)
			if d < 0 {
				d = -d
			}
			if d <= window && (bestNum == 0 || d < bestDist) {
				bestNum = f.num
				bestDist = d
			}
		}
		if bestNum > 0 {
			picked[bestNum] = true
		}
	}

	nums := make([]int, 0, len(picked))
	for n := range picked {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	return nums, nil
}

// copyFile 简单复制文件（用于选帧后重编号，帧是 JPEG 体积小，直接读写在测试版足够）。
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
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
