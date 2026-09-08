package api

import (
	"strings"
	"testing"
	"time"

	"timelapse/config"
)

func TestUpsertTopLevelReplace(t *testing.T) {
	src := `server:
  addr: ":8080"

quick:
  captureMode: "interval"
  name: "快捷录制"

# 注释保留
vision:
  enabled: false
`
	lines := strings.Split(strings.TrimRight(src, "\n"), "\n")
	block := "vision:\n  enabled: true\n  analyzeFrames: 7"
	out := strings.Join(upsertTopLevel(lines, block), "\n")

	if !strings.Contains(out, "enabled: true") || !strings.Contains(out, "analyzeFrames: 7") {
		t.Errorf("vision 块未替换: %s", out)
	}
	if !strings.Contains(out, "captureMode: \"interval\"") {
		t.Errorf("quick 段被误删: %s", out)
	}
	if !strings.Contains(out, "注释保留") {
		t.Errorf("注释被误删: %s", out)
	}
	if strings.Contains(out, "vision:\n  enabled: false") {
		t.Errorf("旧 vision 块仍在: %s", out)
	}
}

func TestUpsertTopLevelAppend(t *testing.T) {
	lines := []string{"server:\n  addr: \":8080\""}
	out := strings.Join(upsertTopLevel(lines, "vision:\n  enabled: true"), "\n")
	if !strings.HasSuffix(out, "vision:\n  enabled: true") {
		t.Errorf("追加失败: %s", out)
	}
}

func TestMarshalVisionReadable(t *testing.T) {
	vc := config.VisionConfig{
		Enabled: true, Provider: "deepseek", BaseURL: "https://api.deepseek.com/v1",
		APIKey: "sk-x", Model: "deepseek-v4-flash-vision-exp", Timeout: 30 * time.Second,
		AnalyzeFrames: 5, MinConfidence: 0.8, FailureStreak: 2, CooldownSeconds: 300,
		Bark: config.BarkConfig{Enabled: true, Key: "k", BaseURL: "http://192.168.1.10:8080"},
	}
	out, err := marshalVision(vc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "timeout: 30s") {
		t.Errorf("timeout 应写成可读 30s: %s", out)
	}
	if !strings.Contains(out, "baseUrl: http://192.168.1.10:8080") {
		t.Errorf("自建 bark baseUrl 丢失: %s", out)
	}
	if !strings.Contains(out, "apiKey: sk-x") {
		t.Errorf("apiKey 丢失: %s", out)
	}
}

func TestIsTopLevel(t *testing.T) {
	if !isTopLevel("vision:") {
		t.Error("vision: 应是顶层")
	}
	if isTopLevel("  enabled: true") {
		t.Error("缩进行不应是顶层")
	}
	if isTopLevel("# comment") {
		t.Error("注释不应是顶层")
	}
	if isTopLevel("") {
		t.Error("空行不应是顶层")
	}
}
