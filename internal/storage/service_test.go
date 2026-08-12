package storage

import (
	"os"
	"path/filepath"
	"testing"

	"timelapse/config"
)

func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Storage.BaseDir = dir
	cfg.Storage.FramesDir = "frames"
	cfg.Storage.VideosDir = "videos"
	return New(cfg), dir
}

func TestSanitizeFilename(t *testing.T) {
	s, _ := newTestService(t)
	cases := map[string]string{
		"植物生长":            "植物生长",
		"植物 生长":           "植物_生长",
		"a/b\\c:d*e?f\"g<h>i|j": "a_b_c_d_e_f_g_h_i_j",
		"  ":              "timelapse",
	}
	for in, want := range cases {
		if got := s.SanitizeFilename(in); got != want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCountFiles(t *testing.T) {
	s, dir := newTestService(t)
	frames := filepath.Join(dir, "frames")
	if err := os.MkdirAll(frames, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		if err := os.WriteFile(filepath.Join(frames, "00000"+string(rune('0'+i))+".jpg"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.CountFiles(frames)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("CountFiles = %d, want 3", n)
	}
	// 不存在的目录返回 0
	n, err = s.CountFiles(filepath.Join(dir, "nope"))
	if err != nil || n != 0 {
		t.Fatalf("CountFiles missing dir = %d, %v; want 0, nil", n, err)
	}
}

func TestFramesDir(t *testing.T) {
	s, _ := newTestService(t)
	got := s.FramesDir(42)
	if filepath.Base(got) != "task-42" || filepath.Base(filepath.Dir(got)) != "frames" {
		t.Fatalf("unexpected frames dir: %s", got)
	}
}
