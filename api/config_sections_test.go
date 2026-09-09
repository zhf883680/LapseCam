package api

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	"timelapse/config"
)

func TestMarshalSectionRoundTrip(t *testing.T) {
	c := config.Default()
	// 调整一些非默认值，验证往返
	c.FFmpeg.ProbeTimeout = 12 * time.Second
	c.FFmpeg.CaptureBackoff = []time.Duration{5 * time.Second, 15 * time.Second}
	c.FFmpeg.EncodeThreads = 3
	c.Quick.CaptureMode = "layer"
	c.Cleanup.VideoRetentionDays = 7
	c.Preview.StartTimeout = 9 * time.Second

	blocks := []struct {
		key string
		fn  func() (string, error)
	}{
		{"server", func() (string, error) { return marshalServer(c.Server) }},
		{"database", func() (string, error) { return marshalDatabase(c.Database) }},
		{"storage", func() (string, error) { return marshalStorage(c.Storage) }},
		{"ffmpeg", func() (string, error) { return marshalFFmpeg(c.FFmpeg) }},
		{"preview", func() (string, error) { return marshalPreview(c.Preview) }},
		{"scheduler", func() (string, error) { return marshalScheduler(c.Scheduler) }},
		{"quick", func() (string, error) { return marshalQuick(c.Quick) }},
		{"cleanup", func() (string, error) { return marshalCleanup(c.Cleanup) }},
	}

	for _, b := range blocks {
		blk, err := b.fn()
		if err != nil {
			t.Fatalf("%s marshal: %v", b.key, err)
		}
		var c2 config.Config
		if err := yaml.Unmarshal([]byte(blk), &c2); err != nil {
			t.Fatalf("%s unmarshal: %v\n%s", b.key, err, blk)
		}
	}

	// 关键字段往返校验
	var c2 config.Config
	blk, _ := marshalFFmpeg(c.FFmpeg)
	_ = yaml.Unmarshal([]byte(blk), &c2)
	if c2.FFmpeg.ProbeTimeout != 12*time.Second || c2.FFmpeg.EncodeThreads != 3 {
		t.Errorf("ffmpeg roundtrip = %+v", c2.FFmpeg)
	}
	blk, _ = marshalQuick(c.Quick)
	_ = yaml.Unmarshal([]byte(blk), &c2)
	if c2.Quick.CaptureMode != "layer" {
		t.Errorf("quick roundtrip captureMode = %q", c2.Quick.CaptureMode)
	}
	blk, _ = marshalCleanup(c.Cleanup)
	_ = yaml.Unmarshal([]byte(blk), &c2)
	if c2.Cleanup.VideoRetentionDays != 7 {
		t.Errorf("cleanup roundtrip = %+v", c2.Cleanup)
	}
}

func TestApplySections(t *testing.T) {
	c := config.Default()

	addr := ":9999"
	base := "/data2"
	applySections(c, configInput{
		Server:  &serverIn{Addr: &addr},
		Storage: &storageIn{BaseDir: &base},
	})
	if c.Server.Addr != ":9999" || c.Storage.BaseDir != "/data2" {
		t.Errorf("apply server/storage = %+v / %+v", c.Server, c.Storage)
	}

	probe := 12
	threads := 2
	applySections(c, configInput{FFmpeg: &ffmpegIn{ProbeTimeoutSec: &probe, EncodeThreads: &threads}})
	if c.FFmpeg.ProbeTimeout != 12*time.Second || c.FFmpeg.EncodeThreads != 2 {
		t.Errorf("apply ffmpeg = %+v", c.FFmpeg)
	}

	mode := "timestamp"
	applySections(c, configInput{Quick: &quickIn{CaptureMode: &mode}})
	if c.Quick.CaptureMode != "timestamp" {
		t.Errorf("apply quick = %+v", c.Quick)
	}
}
