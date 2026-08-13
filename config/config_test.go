package config

import "testing"

// TestCompressionDefaults 锁定压缩相关默认值，防止回退到体积巨大的旧参数。
func TestCompressionDefaults(t *testing.T) {
	cfg := Default()
	if cfg.FFmpeg.EncodePreset != "slow" {
		t.Errorf("EncodePreset = %q, want slow", cfg.FFmpeg.EncodePreset)
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
}

// TestLoadYAML 确保随仓库发布的 config.yaml 能被解析且携带新的压缩参数。
func TestLoadYAML(t *testing.T) {
	cfg, err := Load("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FFmpeg.EncodePreset != "slow" {
		t.Errorf("yaml EncodePreset = %q, want slow", cfg.FFmpeg.EncodePreset)
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
}
