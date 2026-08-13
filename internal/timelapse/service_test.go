package timelapse

import (
	"errors"
	"strings"
	"testing"
	"time"

	"timelapse/config"
	"timelapse/internal/camera"
	"timelapse/internal/database"
	"timelapse/internal/ffmpeg"
	"timelapse/internal/storage"
)

func newTestService(t *testing.T) (*Service, func()) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Storage.BaseDir = dir // 测试数据（帧/标记/日志）全部落到临时目录
	st := storage.New(cfg)
	ff := ffmpeg.New(cfg)
	cam := camera.New(db, cfg, ff)
	s := New(db, cfg, ff, cam, st)
	return s, func() { db.Close() }
}

func TestParseTime(t *testing.T) {
	ok := []string{
		"2026-08-12T08:00:00+08:00",
		"2026-08-12T08:00:00Z",
		"2026-08-12T08:00:00",
		"2026-08-12 08:00:00",
		"2026-08-12 08:00",
		"2026-08-12",
	}
	for _, s := range ok {
		if _, err := parseTime(s); err != nil {
			t.Errorf("parseTime(%q) error: %v", s, err)
		}
	}
	if _, err := parseTime("not-a-time"); err == nil {
		t.Error("expected error for invalid time")
	}
}

func TestTaskLifecycle(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	cam, err := s.cam.Create(camera.CameraInput{Name: "cam", RtspURL: "rtsp://127.0.0.1:1/stream"})
	if err != nil {
		t.Fatal(err)
	}

	// 开始时间在未来 → 任务保持 pending，不触发 worker
	start := time.Now().Add(time.Hour).Format(time.RFC3339)
	end := time.Now().Add(2 * time.Hour).Format(time.RFC3339)
	task, err := s.Create(TaskInput{
		Name:            "植物生长",
		CameraID:        cam.ID,
		IntervalSeconds: 10,
		OutputFPS:       30,
		Width:           1920,
		Height:          1080,
		StartAt:         start,
		EndAt:           &end,
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.ID == 0 || task.Status != StatusPending {
		t.Fatalf("bad task: %+v", task)
	}
	if task.CameraName != "cam" {
		t.Fatalf("cameraName not joined: %+v", task)
	}

	// 被未完成任务引用的摄像头不可删除
	active, err := s.HasActiveTasks(cam.ID)
	if err != nil || !active {
		t.Fatalf("HasActiveTasks = %v, %v; want true", active, err)
	}

	// 停止 pending 任务 → stopped
	if err := s.Stop(task.ID); err != nil {
		t.Fatal(err)
	}
	t2, _ := s.Get(task.ID)
	if t2.Status != StatusStopped {
		t.Fatalf("status = %s, want stopped", t2.Status)
	}
	active, _ = s.HasActiveTasks(cam.ID)
	if active {
		t.Fatal("HasActiveTasks should be false after stop")
	}

	// 删除任务
	if err := s.Delete(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(task.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestCreateValidation(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	cam, err := s.cam.Create(camera.CameraInput{Name: "cam", RtspURL: "rtsp://127.0.0.1:1/stream"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(time.Hour).Format(time.RFC3339)

	cases := []TaskInput{
		{Name: "", CameraID: cam.ID, IntervalSeconds: 10, OutputFPS: 30, Width: 1920, Height: 1080, StartAt: start},
		{Name: "x", CameraID: 9999, IntervalSeconds: 10, OutputFPS: 30, Width: 1920, Height: 1080, StartAt: start},
		{Name: "x", CameraID: cam.ID, IntervalSeconds: 0, OutputFPS: 30, Width: 1920, Height: 1080, StartAt: start},
		{Name: "x", CameraID: cam.ID, IntervalSeconds: 10, OutputFPS: 0, Width: 1920, Height: 1080, StartAt: start},
		{Name: "x", CameraID: cam.ID, IntervalSeconds: 10, OutputFPS: 30, Width: 0, Height: 1080, StartAt: start},
		{Name: "x", CameraID: cam.ID, IntervalSeconds: 10, OutputFPS: 30, Width: 1920, Height: 1080, StartAt: "bad-time"},
	}
	for i, in := range cases {
		if _, err := s.Create(in); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestVideoOutputPath(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	start := time.Date(2026, 8, 12, 0, 0, 0, 0, time.Local)
	task := Task{ID: 7, Name: "植物 生长", StartAt: start}
	got := s.videoOutputPath(task)
	if !strings.Contains(got, "植物_生长_2026-08-12.mp4") {
		t.Fatalf("unexpected video path: %s", got)
	}
}
