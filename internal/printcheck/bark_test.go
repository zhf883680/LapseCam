package printcheck

import (
	"strings"
	"testing"

	"timelapse/config"
	"timelapse/internal/vision"
)

func TestBuildBarkPayload(t *testing.T) {
	c := Check{Status: vision.StatusSpaghetti, Confidence: 0.94, Reason: "模型顶部大量无规则挤出丝"}
	b := config.BarkConfig{Key: "test-key", Group: "打印机", Level: "critical", Volume: 10}
	p := buildBarkPayload(c, b)
	if p.DeviceKey != "test-key" || p.Level != "critical" {
		t.Errorf("payload = %+v", p)
	}
	if p.Volume != 10 {
		t.Errorf("volume = %d, want 10", p.Volume)
	}
	if !strings.Contains(p.Title, "炒面") {
		t.Errorf("title = %q, want 含炒面", p.Title)
	}
	if !strings.Contains(p.Body, "挤出丝") || !strings.Contains(p.Body, "94%") {
		t.Errorf("body = %q", p.Body)
	}
	if p.Group != "打印机" {
		t.Errorf("group = %q", p.Group)
	}

	// 默认 group、无 reason 时的兜底文案
	p2 := buildBarkPayload(Check{Status: vision.StatusClog, Confidence: 0.9}, config.BarkConfig{Key: "k"})
	if p2.Group != "LapseCam" {
		t.Errorf("default group = %q", p2.Group)
	}
	if p2.Body == "" || !strings.Contains(p2.Body, "90%") {
		t.Errorf("fallback body = %q", p2.Body)
	}
}

func TestStatusLabel(t *testing.T) {
	cases := map[string]string{
		vision.StatusNormal:          "正常",
		vision.StatusSpaghetti:       "炒面",
		vision.StatusClog:            "堵头",
		vision.StatusObjectDisplaced: "打印件被拖走",
		"whatever":                   "whatever",
	}
	for in, want := range cases {
		if got := statusLabel(in); got != want {
			t.Errorf("statusLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
