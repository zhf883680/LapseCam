package timelapse

import (
	"testing"

	"timelapse/internal/camera"
)

func TestTaskAIEnabledDefaultOffAndToggle(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	cam, err := s.cam.Create(camera.CameraInput{Name: "cam1", RtspURL: "rtsp://127.0.0.1:1/stream"})
	if err != nil {
		t.Fatal(err)
	}

	// 默认不开启（vision.aiEnabledByDefault=false）
	task, _, err := s.QuickStart()
	if err != nil {
		t.Fatal(err)
	}
	if task.AIEnabled {
		t.Fatal("task should default to AI disabled")
	}
	if on, _ := s.TaskAIEnabled(task.ID); on {
		t.Fatal("TaskAIEnabled should be false by default")
	}

	// 手动开启后应为 true，关闭后为 false
	if err := s.SetAIEnabled(task.ID, true); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.TaskAIEnabled(task.ID); !on {
		t.Fatal("TaskAIEnabled should be true after enable")
	}
	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.AIEnabled {
		t.Fatal("Get should reflect AIEnabled=true")
	}

	if err := s.SetAIEnabled(task.ID, false); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.TaskAIEnabled(task.ID); on {
		t.Fatal("TaskAIEnabled should be false after disable")
	}

	// 未引用的摄像头变量防止编译错误
	_ = cam
}

func TestCreateHonorsAIEnabledOverride(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	cam, err := s.cam.Create(camera.CameraInput{Name: "cam1", RtspURL: "rtsp://127.0.0.1:1/stream"})
	if err != nil {
		t.Fatal(err)
	}

	on := true
	task, err := s.Create(TaskInput{
		Name: "t", CameraID: cam.ID, IntervalSeconds: 5, OutputFPS: 30, Width: 1280, Height: 720,
		StartAt: "2026-01-01 00:00:00", AIEnabled: &on,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !task.AIEnabled {
		t.Fatal("Create should honor AIEnabled=true override")
	}
}
