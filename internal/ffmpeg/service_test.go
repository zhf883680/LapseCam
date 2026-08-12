package ffmpeg

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"timelapse/config"
)

func TestBuildRTSPURL(t *testing.T) {
	cases := []struct {
		raw, user, pass, want string
	}{
		{"rtsp://192.168.1.100:554/stream1", "admin", "123", "rtsp://admin:123@192.168.1.100:554/stream1"},
		{"rtsp://192.168.1.100:554/stream1", "admin", "", "rtsp://admin@192.168.1.100:554/stream1"},
		{"rtsp://192.168.1.100:554/stream1", "", "", "rtsp://192.168.1.100:554/stream1"},
		{"rtsp://192.168.1.100:554/a b", "u", "p@ss:w", "rtsp://u:p%40ss%3Aw@192.168.1.100:554/a%20b"},
	}
	for _, c := range cases {
		if got := BuildRTSPURL(c.raw, c.user, c.pass); got != c.want {
			t.Errorf("BuildRTSPURL(%q,%q,%q) = %q, want %q", c.raw, c.user, c.pass, got, c.want)
		}
	}
}

// TestEncode 用本地生成的 JPEG 帧验证 JPEG→H.264 MP4 编码链路。
func TestEncode(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()

	// 生成 5 帧 320x240 测试图
	gen := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=1",
		"-frames:v", "5", filepath.Join(dir, "%02d.jpg"))
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generate frames: %v\n%s", err, out)
	}

	cfg := config.Default()
	s := New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	out := filepath.Join(dir, "out.mp4")
	err := s.Encode(ctx, filepath.Join(dir, "%02d.jpg"), out, 30, 640, 360, "ultrafast", 20, os.Stderr)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("encoded file is empty")
	}
	t.Logf("encoded mp4 size = %d bytes", info.Size())
}
