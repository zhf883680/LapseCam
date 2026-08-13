package preview

import (
	"testing"

	"timelapse/config"
	"timelapse/internal/camera"
)

// TestSourceURL 验证 go2rtc 源地址拼接：
// fragment 多参数必须用 "#" 分隔（go2rtc 约定），不能用 "&"。
func TestSourceURL(t *testing.T) {
	cfg := config.Default()
	s := New(cfg)

	cam := camera.Camera{
		ID:       1,
		RtspURL:  "rtsp://192.168.0.120:554/stream1&channel=2",
		Username: "admin",
		Password: "secret",
	}

	got := s.sourceURL(cam)
	want := "rtsp://admin:secret@192.168.0.120:554/stream1&channel=2#transport=tcp#media=video"
	if got != want {
		t.Fatalf("sourceURL mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestSourceURLNoOpts(t *testing.T) {
	cfg := config.Default()
	cfg.Preview.RTSPTransport = ""
	cfg.Preview.Media = ""
	s := New(cfg)

	cam := camera.Camera{
		RtspURL: "rtsp://192.168.0.120:554/stream1",
	}

	if got := s.sourceURL(cam); got != "rtsp://192.168.0.120:554/stream1" {
		t.Fatalf("unexpected sourceURL: %s", got)
	}
}
