package timelapse

import (
	"errors"
	"testing"

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
