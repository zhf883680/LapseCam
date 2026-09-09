package printcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"timelapse/config"
	"timelapse/internal/vision"
)

// barkPayload Bark POST /push 的 JSON 请求体（教程：https://bark.day.app/#/tutorial）。
type barkPayload struct {
	DeviceKey string `json:"device_key"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Group     string `json:"group,omitempty"`
	Level     string `json:"level,omitempty"`
	Volume    int    `json:"volume,omitempty"` // 重要警告音量 0-10
}

// sendBark 确认故障时推一条 Bark 文字通知（不带图片）。
// 与 Webhook 相互独立：bark.enabled=true 才发，失败只记日志。
func (s *Service) sendBark(c Check) {
	b := s.cfg.Vision.Bark
	key := strings.TrimSpace(b.Key)
	if !b.Enabled || key == "" {
		return
	}
	base := strings.TrimRight(strings.TrimSpace(b.BaseURL), "/")
	if base == "" {
		base = "https://api.day.app"
	}
	payload := buildBarkPayload(c, b)
	raw, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[printcheck] bark marshal failed: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/push", bytes.NewReader(raw))
	if err != nil {
		log.Printf("[printcheck] bark request failed: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[printcheck] bark send failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("[printcheck] bark http %d", resp.StatusCode)
	}
}

// buildBarkPayload 组装 Bark 推送内容（纯函数，便于单测）。
func buildBarkPayload(c Check, b config.BarkConfig) barkPayload {
	payload := barkPayload{
		DeviceKey: strings.TrimSpace(b.Key),
		Title:     "3D 打印异常：" + statusLabel(c.Status),
		Group:     strings.TrimSpace(b.Group),
		Level:     b.Level,
		Volume:    b.Volume,
	}
	if payload.Group == "" {
		payload.Group = "LapseCam"
	}
	if reason := strings.TrimSpace(c.Reason); reason != "" {
		payload.Body = reason + "（置信度 " + pct(c.Confidence) + "）"
	} else {
		payload.Body = "AI 连续多次判断打印异常（置信度 " + pct(c.Confidence) + "）"
	}
	return payload
}

// statusLabel 把英文状态码转成人话（Bark 标题用）。
func statusLabel(s string) string {
	switch s {
	case vision.StatusNormal:
		return "正常"
	case vision.StatusSpaghetti:
		return "炒面"
	case vision.StatusClog:
		return "堵头"
	case vision.StatusObjectDisplaced:
		return "打印件被拖走"
	case vision.StatusNozzleCollision:
		return "喷嘴碰撞"
	case vision.StatusMaterialBuildup:
		return "积料"
	default:
		return s
	}
}

func pct(v float64) string {
	return fmt.Sprintf("%.0f%%", v*100)
}
