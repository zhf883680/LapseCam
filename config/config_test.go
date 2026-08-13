package config

import (
	"os"
	"testing"
)

// TestCompressionDefaults 锁定压缩相关默认值，防止回退到体积巨大的旧参数。
func TestCompressionDefaults(t *testing.T) {
	cfg := Default()
	if cfg.FFmpeg.EncodePreset != "veryfast" {
		t.Errorf("EncodePreset = %q, want veryfast", cfg.FFmpeg.EncodePreset)
	}
	if cfg.FFmpeg.EncodeCRF != 26 {
		t.Errorf("EncodeCRF = %d, want 26", cfg.FFmpeg.EncodeCRF)
	}
	if cfg.FFmpeg.EncodeMaxRateKbps != 4000 {
		t.Errorf("EncodeMaxRateKbps = %d, want 4000", cfg.FFmpeg.EncodeMaxRateKbps)
	}
	if cfg.Quick.Width != 1280 || cfg.Quick.Height != 720 {
		t.Errorf("quick 默认分辨率 = %dx%d, want 1280x720", cfg.Quick.Width, cfg.Quick.Height)
	}
	if cfg.Preview.Enabled != true || cfg.Preview.BasePath != "/go2rtc" || cfg.Preview.Addr != "127.0.0.1:1984" {
		t.Errorf("preview 默认配置异常: %+v", cfg.Preview)
	}
}

// TestLoadYAML 确保随仓库发布的 config.yaml 能被解析且携带新的压缩参数。
func TestLoadYAML(t *testing.T) {
	cfg, err := Load("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FFmpeg.EncodePreset != "veryfast" {
		t.Errorf("yaml EncodePreset = %q, want veryfast", cfg.FFmpeg.EncodePreset)
	}
	if cfg.FFmpeg.EncodeCRF != 26 {
		t.Errorf("yaml EncodeCRF = %d, want 26", cfg.FFmpeg.EncodeCRF)
	}
	if cfg.FFmpeg.EncodeMaxRateKbps != 4000 {
		t.Errorf("yaml EncodeMaxRateKbps = %d, want 4000", cfg.FFmpeg.EncodeMaxRateKbps)
	}
	if cfg.Quick.Width != 1280 || cfg.Quick.Height != 720 {
		t.Errorf("yaml quick 分辨率 = %dx%d, want 1280x720", cfg.Quick.Width, cfg.Quick.Height)
	}
	if !cfg.Preview.Enabled || cfg.Preview.BasePath != "/go2rtc" || cfg.Preview.RTSPTransport != "tcp" {
		t.Errorf("yaml preview 配置异常: %+v", cfg.Preview)
	}
	if !cfg.Cleanup.Enabled || cfg.Cleanup.IntervalHours != 24 || !cfg.Cleanup.RemoveFramesAfterEncode || cfg.Cleanup.VideoRetentionDays != 0 || !cfg.Cleanup.RemoveOrphans {
		t.Fatalf("yaml cleanup 配置异常: %+v", cfg.Cleanup)
	}
}

// TestPreviewDisabled 确保用户显式设置 preview.enabled=false 时不会被默认值重置回 true。
func TestPreviewDisabled(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/c.yaml"
	if err := os.WriteFile(path, []byte("preview:\n  enabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Preview.Enabled {
		t.Fatal("preview.enabled=false 被默认值覆盖回 true")
	}
}

// TestCaptureModeNormalize 锁定 captureMode 默认值与非法的回退行为。
func TestCaptureModeNormalize(t *testing.T) {
	cfg := Default()
	if cfg.Quick.CaptureMode != CaptureModeInterval {
		t.Fatalf("默认 captureMode = %q, want interval", cfg.Quick.CaptureMode)
	}
	if cfg.Quick.LayerWindowSeconds != 5 {
		t.Fatalf("默认 layerWindowSeconds = %v, want 5", cfg.Quick.LayerWindowSeconds)
	}

	dir := t.TempDir()
	for _, mode := range []string{"layer", "timestamp", "interval"} {
		path := dir + "/m-" + mode + ".yaml"
		if err := os.WriteFile(path, []byte("quick:\n  captureMode: "+mode+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		c, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if c.Quick.CaptureMode != mode {
			t.Fatalf("captureMode=%s → %s", mode, c.Quick.CaptureMode)
		}
	}

	// 非法值回退默认
	path := dir + "/bad.yaml"
	if err := os.WriteFile(path, []byte("quick:\n  captureMode: bogus\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Quick.CaptureMode != CaptureModeInterval {
		t.Fatalf("非法 captureMode 应回退 interval, got %q", c.Quick.CaptureMode)
	}
}

// TestCleanupDefaults 锁定清理功能默认值。
func TestCleanupDefaults(t *testing.T) {
	cfg := Default()
	if !cfg.Cleanup.Enabled || cfg.Cleanup.IntervalHours != 24 ||
		!cfg.Cleanup.RemoveFramesAfterEncode || cfg.Cleanup.VideoRetentionDays != 0 || !cfg.Cleanup.RemoveOrphans {
		t.Fatalf("cleanup 默认配置异常: %+v", cfg.Cleanup)
	}
}

// TestCleanupIntervalHoursNormalize intervalHours<=0 回退默认 24。
func TestCleanupIntervalHoursNormalize(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/c.yaml"
	if err := os.WriteFile(path, []byte("cleanup:\n  intervalHours: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cleanup.IntervalHours != 24 {
		t.Fatalf("intervalHours = %d, want 24", cfg.Cleanup.IntervalHours)
	}
}
