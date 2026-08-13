package timelapse

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"timelapse/config"
	"timelapse/internal/camera"
)

func TestQuickStartNoCamera(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	if _, _, err := s.QuickStart(); !errors.Is(err, ErrNoCamera) {
		t.Fatalf("expected ErrNoCamera, got %v", err)
	}
}

func TestQuickStartCreatesRunningTask(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	cam, err := s.cam.Create(camera.CameraInput{Name: "cam1", RtspURL: "rtsp://127.0.0.1:1/stream"})
	if err != nil {
		t.Fatal(err)
	}

	task, already, err := s.QuickStart()
	if err != nil {
		t.Fatal(err)
	}
	if already {
		t.Fatal("first QuickStart should not be 'already recording'")
	}
	if task.ID == 0 {
		t.Fatal("expected non-zero task id")
	}
	if task.CameraID != cam.ID {
		t.Fatalf("expected camera %d, got %d", cam.ID, task.CameraID)
	}
	if task.Status != StatusRunning {
		t.Fatalf("expected running, got %s", task.Status)
	}
	if task.Name != "快捷录制" {
		t.Fatalf("expected default name 快捷录制, got %s", task.Name)
	}
}

func TestQuickStartIdempotent(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	if _, err := s.cam.Create(camera.CameraInput{Name: "cam1", RtspURL: "rtsp://127.0.0.1:1/stream"}); err != nil {
		t.Fatal(err)
	}

	first, already, err := s.QuickStart()
	if err != nil {
		t.Fatal(err)
	}
	if already {
		t.Fatal("first QuickStart should not be 'already recording'")
	}

	second, already2, err := s.QuickStart()
	if err != nil {
		t.Fatal(err)
	}
	if !already2 {
		t.Fatal("second QuickStart should report already recording")
	}
	if second.ID != first.ID {
		t.Fatalf("expected same task id %d, got %d", first.ID, second.ID)
	}
}

func TestQuickStop(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	if _, err := s.cam.Create(camera.CameraInput{Name: "cam1", RtspURL: "rtsp://127.0.0.1:1/stream"}); err != nil {
		t.Fatal(err)
	}
	task, _, err := s.QuickStart()
	if err != nil {
		t.Fatal(err)
	}

	stopped, found, err := s.QuickStop()
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if stopped.ID != task.ID {
		t.Fatalf("expected task %d, got %d", task.ID, stopped.ID)
	}
	switch stopped.Status {
	case StatusStopping, StatusFailed, StatusCompleted:
	default:
		t.Fatalf("unexpected status after stop: %s", stopped.Status)
	}
}

func TestQuickStopNoRecord(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	if _, found, err := s.QuickStop(); err != nil {
		t.Fatal(err)
	} else if found {
		t.Fatal("expected found=false")
	}
}

func TestQuickSnapshotNotLayerMode(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	// 默认 captureMode=interval → 应拒绝逐层截图
	if _, err := s.QuickSnapshot(1); !errors.Is(err, ErrNotLayerMode) {
		t.Fatalf("expected ErrNotLayerMode, got %v", err)
	}
}

func TestQuickSnapshotNoTask(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	s.cfg.Quick.CaptureMode = config.CaptureModeLayer
	if _, err := s.QuickSnapshot(1); !errors.Is(err, ErrNoQuickTask) {
		t.Fatalf("expected ErrNoQuickTask, got %v", err)
	}
}

func TestQuickRecordLayerNoTask(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	if _, err := s.QuickRecordLayer(1); !errors.Is(err, ErrNoQuickTask) {
		t.Fatalf("expected ErrNoQuickTask, got %v", err)
	}
}

func TestQuickRecordLayerWritesMarker(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	if _, err := s.cam.Create(camera.CameraInput{Name: "cam1", RtspURL: "rtsp://127.0.0.1:1/stream"}); err != nil {
		t.Fatal(err)
	}
	task, _, err := s.QuickStart()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.QuickRecordLayer(3); err != nil {
		t.Fatal(err)
	}
	if _, err := s.QuickRecordLayer(4); err != nil {
		t.Fatal(err)
	}

	markers, err := loadLayerMarkersFile(s.storage.MarkersFile(task.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(markers) != 2 {
		t.Fatalf("expected 2 markers, got %d: %+v", len(markers), markers)
	}
	if markers[0].Layer != 3 || markers[1].Layer != 4 {
		t.Fatalf("bad markers: %+v", markers)
	}
	if !markers[1].Time.After(markers[0].Time) {
		t.Fatalf("marker times not increasing: %+v", markers)
	}
}

// setupSelectTest 准备 timestamp 选帧测试环境：帧 1..5（mtime 间隔 5s）+ 两个层标记。
func setupSelectTest(t *testing.T) (*Service, Task, func()) {
	t.Helper()
	s, cleanup := newTestService(t)
	s.cfg.Quick.CaptureMode = config.CaptureModeTimestamp
	s.cfg.Quick.LayerWindowSeconds = 5

	cam, err := s.cam.Create(camera.CameraInput{Name: "cam", RtspURL: "rtsp://127.0.0.1:1/stream"})
	if err != nil {
		t.Fatal(err)
	}
	// 未来开始的任务：不启动 worker，避免真的跑 ffmpeg
	task, err := s.Create(TaskInput{
		Name:            s.cfg.Quick.Name,
		CameraID:        cam.ID,
		IntervalSeconds: 5,
		OutputFPS:       30,
		Width:           1280,
		Height:          720,
		StartAt:         time.Now().Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}

	framesDir := s.storage.FramesDir(task.ID)
	if err := s.storage.EnsureDir(framesDir); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	for i := 1; i <= 5; i++ {
		path := filepath.Join(framesDir, fmt.Sprintf("%06d.jpg", i))
		if err := os.WriteFile(path, []byte("fake-jpeg"), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := base.Add(time.Duration(i*5) * time.Second)
		if err := os.Chtimes(path, mt, mt); err != nil {
			t.Fatal(err)
		}
	}

	// 标记 A@16s、B@24s
	for _, lm := range []layerMarker{
		{Time: base.Add(16 * time.Second), Layer: 1},
		{Time: base.Add(24 * time.Second), Layer: 2},
	} {
		if err := appendLayerMarker(s.storage.MarkersFile(task.ID), lm); err != nil {
			t.Fatal(err)
		}
	}
	return s, task, cleanup
}

func TestSelectFramesByMarkers(t *testing.T) {
	s, task, cleanup := setupSelectTest(t)
	defer cleanup()

	// 偏移 0：A@16 → 帧3(15s)，B@24 → 帧5(25s)
	s.cfg.Quick.LayerOffsetSeconds = 0
	selected, err := s.selectFramesByMarkers(task)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{3, 5}
	if len(selected) != len(want) || selected[0] != want[0] || selected[1] != want[1] {
		t.Fatalf("offset 0: selected %v, want %v", selected, want)
	}

	// 偏移 -3（层变化上报晚于停车）：A 目标 13s → 帧3(15s)，B 目标 21s → 帧4(20s)
	s.cfg.Quick.LayerOffsetSeconds = -3
	selected, err = s.selectFramesByMarkers(task)
	if err != nil {
		t.Fatal(err)
	}
	want = []int{3, 4}
	if len(selected) != len(want) || selected[0] != want[0] || selected[1] != want[1] {
		t.Fatalf("offset -3: selected %v, want %v", selected, want)
	}
}

func TestSelectFramesByMarkersOnlyQuickTask(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	s.cfg.Quick.CaptureMode = config.CaptureModeTimestamp
	cam, err := s.cam.Create(camera.CameraInput{Name: "cam", RtspURL: "rtsp://127.0.0.1:1/stream"})
	if err != nil {
		t.Fatal(err)
	}
	// 普通任务（名字不是快捷任务）不应走选帧
	task, err := s.Create(TaskInput{
		Name:            "普通延时",
		CameraID:        cam.ID,
		IntervalSeconds: 5,
		OutputFPS:       30,
		Width:           1280,
		Height:          720,
		StartAt:         time.Now().Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := s.selectFramesByMarkers(task)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 0 {
		t.Fatalf("normal task should not select frames, got %v", selected)
	}
}
