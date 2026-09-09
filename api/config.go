package api

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"timelapse/config"
)

// configView 页面可编辑的配置（只暴露 AI/打印监控相关，密码/Key 不回传明文）。
type configView struct {
	Path        string     `json:"path"`
	CaptureMode string     `json:"captureMode"`
	AIEnabled   bool       `json:"aiEnabled"`
	Vision      visionView `json:"vision"`
	Others      string     `json:"others"` // 除 vision 外的完整配置（YAML 原文，供“其他设置”编辑）
}

type visionView struct {
	Enabled            bool    `json:"enabled"`
	Provider           string  `json:"provider"`
	BaseURL            string  `json:"baseUrl"`
	Model              string  `json:"model"`
	APIKey             string  `json:"apiKey"` // 只读：始终空
	APIKeySet          bool    `json:"apiKeySet"`
	TimeoutSec         int     `json:"timeoutSec"`
	Detail             string  `json:"detail"`
	DisableThinking    bool    `json:"disableThinking"`
	AnalyzeFrames      int     `json:"analyzeFrames"`
	AnalyzeIntervalSec int     `json:"analyzeIntervalSec"`
	MinConfidence      float64 `json:"minConfidence"`
	FailureStreak      int     `json:"failureStreak"`
	CooldownSeconds    int     `json:"cooldownSeconds"`

	WebhookEnabled bool   `json:"webhookEnabled"`
	WebhookURL     string `json:"webhookUrl"`

	BarkEnabled bool   `json:"barkEnabled"`
	BarkKey     string `json:"barkKey"` // 只读：始终空
	BarkKeySet  bool   `json:"barkKeySet"`
	BarkGroup   string `json:"barkGroup"`
	BarkLevel   string `json:"barkLevel"`
	BarkVolume  int    `json:"barkVolume"`
	BarkBaseURL string `json:"barkBaseUrl"`
}

// configInput PUT /api/config 的入参（字段缺省 = 不修改）。
type configInput struct {
	CaptureMode *string   `json:"captureMode"`
	Vision      *visionIn `json:"vision"`
	Others      *string   `json:"others"` // 非空时：整体替换除 vision 外的配置段（YAML 原文）
}

type visionIn struct {
	Enabled            *bool    `json:"enabled"`
	Provider           *string  `json:"provider"`
	BaseURL            *string  `json:"baseUrl"`
	Model              *string  `json:"model"`
	APIKey             *string  `json:"apiKey"` // 空 = 保持原值
	TimeoutSec         *int     `json:"timeoutSec"`
	Detail             *string  `json:"detail"`
	DisableThinking    *bool    `json:"disableThinking"`
	AnalyzeFrames      *int     `json:"analyzeFrames"`
	AnalyzeIntervalSec *int     `json:"analyzeIntervalSec"`
	MaxChecksPerTask   *int     `json:"maxChecksPerTask"`
	MinConfidence      *float64 `json:"minConfidence"`
	FailureStreak      *int     `json:"failureStreak"`
	CooldownSeconds    *int     `json:"cooldownSeconds"`

	WebhookEnabled *bool   `json:"webhookEnabled"`
	WebhookURL     *string `json:"webhookUrl"`

	BarkEnabled *bool   `json:"barkEnabled"`
	BarkKey     *string `json:"barkKey"`
	BarkGroup   *string `json:"barkGroup"`
	BarkLevel   *string `json:"barkLevel"`
	BarkVolume  *int    `json:"barkVolume"`
	BarkBaseURL *string `json:"barkBaseUrl"`
}

// getConfig 返回当前生效的 AI/监控配置（用于页面表单回填）。
func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	vc := s.cfg.Vision
	view := configView{
		Path:        s.cfgPath,
		CaptureMode: s.cfg.Quick.CaptureMode,
		AIEnabled:   s.pc != nil && s.pc.Enabled(),
		Others:      s.othersRaw(),
		Vision: visionView{
			Enabled:            vc.Enabled,
			Provider:           vc.Provider,
			BaseURL:            vc.BaseURL,
			Model:              vc.Model,
			APIKeySet:          vc.APIKey != "",
			TimeoutSec:         int(vc.Timeout / time.Second),
			Detail:             vc.Detail,
			DisableThinking:    vc.DisableThinking,
			AnalyzeFrames:      vc.AnalyzeFrames,
			AnalyzeIntervalSec: vc.AnalyzeIntervalSeconds,
			MinConfidence:      vc.MinConfidence,
			FailureStreak:      vc.FailureStreak,
			CooldownSeconds:    vc.CooldownSeconds,
			WebhookEnabled:     vc.Webhook.Enabled,
			WebhookURL:         vc.Webhook.URL,
			BarkEnabled:        vc.Bark.Enabled,
			BarkKeySet:         vc.Bark.Key != "",
			BarkGroup:          vc.Bark.Group,
			BarkLevel:          vc.Bark.Level,
			BarkBaseURL:        vc.Bark.BaseURL,
		},
	}
	writeJSON(w, http.StatusOK, view)
}

// updateConfig 把页面提交的 AI/监控配置写回 config 文件（保留其它段落），重启后生效。
func (s *Server) updateConfig(w http.ResponseWriter, r *http.Request) {
	var in configInput
	if err := readJSON(w, r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.applyConfigInput(in); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message": "配置已保存，重启服务后生效",
		"restart": "/api/config/restart",
	})
}

// restartConfig 保存配置后触发重启：systemd/Docker 会自动拉起新进程。
func (s *Server) restartConfig(w http.ResponseWriter, r *http.Request) {
	log.Println("[config] 收到重启请求，3 秒后退出（由 systemd/Docker 自动拉起）")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"code":0,"message":"正在重启..."}`))
	go func() {
		time.Sleep(3 * time.Second)
		os.Exit(0)
	}()
}

// applyConfigInput 合并入参并写回 config 文件。
func (s *Server) applyConfigInput(in configInput) error {
	path := s.cfgPath
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")

	// 其它功能配置整段替换（YAML 原文，不含 vision，由这里自动合并回来）
	if in.Others != nil {
		return s.applyOthersConfig(raw, in)
	}

	// quick.captureMode（只改这一行，保留 quick 其它参数）
	if in.CaptureMode != nil {
		mode := strings.TrimSpace(*in.CaptureMode)
		switch mode {
		case config.CaptureModeInterval, config.CaptureModeLayer, config.CaptureModeTimestamp:
		default:
			return fmt.Errorf("captureMode 只能是 interval/layer/timestamp")
		}
		replaced := false
		for i := 0; i < len(lines); i++ {
			if !strings.HasPrefix(lines[i], "quick:") {
				continue
			}
			for j := i + 1; j < len(lines); j++ {
				if isTopLevel(lines[j]) {
					break
				}
				if strings.HasPrefix(strings.TrimSpace(lines[j]), "captureMode:") {
					lines[j] = indentLine(lines[j], fmt.Sprintf("captureMode: %q", mode))
					replaced = true
				}
			}
			break
		}
		if !replaced {
			lines = append(lines, "quick:", fmt.Sprintf("  captureMode: %q", mode))
		}
	}

	// vision 整块（含 bark/webhook/策略）重写
	if in.Vision != nil {
		vc := s.cfg.Vision // 以当前生效配置为底
		v := in.Vision
		if v.Enabled != nil {
			vc.Enabled = *v.Enabled
		}
		if v.Provider != nil {
			vc.Provider = *v.Provider
		}
		if v.BaseURL != nil {
			vc.BaseURL = *v.BaseURL
		}
		if v.Model != nil {
			vc.Model = *v.Model
		}
		if v.TimeoutSec != nil && *v.TimeoutSec > 0 {
			vc.Timeout = time.Duration(*v.TimeoutSec) * time.Second
		}
		if v.Detail != nil {
			vc.Detail = *v.Detail
		}
		if v.DisableThinking != nil {
			vc.DisableThinking = *v.DisableThinking
		}
		if v.AnalyzeFrames != nil && *v.AnalyzeFrames > 0 {
			vc.AnalyzeFrames = *v.AnalyzeFrames
		}
		if v.AnalyzeIntervalSec != nil && *v.AnalyzeIntervalSec >= 0 {
			vc.AnalyzeIntervalSeconds = *v.AnalyzeIntervalSec
		}
		if v.MaxChecksPerTask != nil && *v.MaxChecksPerTask >= 0 {
			vc.MaxChecksPerTask = *v.MaxChecksPerTask
		}
		if v.MinConfidence != nil && *v.MinConfidence > 0 {
			vc.MinConfidence = *v.MinConfidence
		}
		if v.FailureStreak != nil && *v.FailureStreak > 0 {
			vc.FailureStreak = *v.FailureStreak
		}
		if v.CooldownSeconds != nil && *v.CooldownSeconds >= 0 {
			vc.CooldownSeconds = *v.CooldownSeconds
		}
		if v.WebhookEnabled != nil {
			vc.Webhook.Enabled = *v.WebhookEnabled
		}
		if v.WebhookURL != nil {
			vc.Webhook.URL = *v.WebhookURL
		}
		if v.BarkEnabled != nil {
			vc.Bark.Enabled = *v.BarkEnabled
		}
		if v.BarkGroup != nil {
			vc.Bark.Group = *v.BarkGroup
		}
		if v.BarkLevel != nil {
			vc.Bark.Level = *v.BarkLevel
		}
		if v.BarkVolume != nil && *v.BarkVolume >= 0 {
			vc.Bark.Volume = *v.BarkVolume
		}
		if v.BarkBaseURL != nil {
			vc.Bark.BaseURL = *v.BarkBaseURL
		}
		// Key：为空表示“不修改”，沿用文件里已存的（不把环境变量的 key 落盘）
		stored := storedKeys(raw)
		if v.APIKey != nil && strings.TrimSpace(*v.APIKey) != "" {
			vc.APIKey = strings.TrimSpace(*v.APIKey)
		} else {
			vc.APIKey = stored.visionKey
		}
		if v.BarkKey != nil && strings.TrimSpace(*v.BarkKey) != "" {
			vc.Bark.Key = strings.TrimSpace(*v.BarkKey)
		} else {
			vc.Bark.Key = stored.barkKey
		}

		block, err := marshalVision(vc)
		if err != nil {
			return err
		}
		lines = upsertTopLevel(lines, block)
	}

	out := []byte(strings.Join(lines, "\n"))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return fmt.Errorf("write config tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

type storedKeysT struct {
	visionKey string
	barkKey   string
}

// storedKeys 从配置文件里读出已保存的 vision.apiKey / bark.key（不回传、仅合并用）。
func storedKeys(raw []byte) storedKeysT {
	var cur struct {
		Vision struct {
			APIKey string `yaml:"apiKey"`
			Bark   struct {
				Key string `yaml:"key"`
			} `yaml:"bark"`
		} `yaml:"vision"`
	}
	_ = yaml.Unmarshal(raw, &cur)
	return storedKeysT{visionKey: cur.Vision.APIKey, barkKey: cur.Vision.Bark.Key}
}

// visionYAML 用于把 vision 配置写成可读的 YAML（timeout 用 "30s" 形式，而非纳秒数字）。
type visionYAML struct {
	Enabled                bool        `yaml:"enabled"`
	Provider               string      `yaml:"provider"`
	BaseURL                string      `yaml:"baseUrl"`
	APIKey                 string      `yaml:"apiKey"`
	Model                  string      `yaml:"model"`
	Timeout                string      `yaml:"timeout"`
	Detail                 string      `yaml:"detail"`
	DisableThinking        bool        `yaml:"disableThinking"`
	AnalyzeFrames          int         `yaml:"analyzeFrames"`
	AnalyzeIntervalSeconds int         `yaml:"analyzeIntervalSeconds"`
	MinConfidence          float64     `yaml:"minConfidence"`
	FailureStreak          int         `yaml:"failureStreak"`
	CooldownSeconds        int         `yaml:"cooldownSeconds"`
	Webhook                webhookYAML `yaml:"webhook"`
	Bark                   barkYAML    `yaml:"bark"`
}

type webhookYAML struct {
	Enabled bool   `yaml:"enabled"`
	URL     string `yaml:"url"`
}

type barkYAML struct {
	Enabled bool   `yaml:"enabled"`
	Key     string `yaml:"key"`
	Group   string `yaml:"group"`
	Level   string `yaml:"level"`
	Volume  int    `yaml:"volume"`
	BaseURL string `yaml:"baseUrl"`
}

func marshalVision(vc config.VisionConfig) (string, error) {
	sec := int(vc.Timeout / time.Second)
	if sec <= 0 {
		sec = 30
	}
	v := visionYAML{
		Enabled:                vc.Enabled,
		Provider:               vc.Provider,
		BaseURL:                vc.BaseURL,
		APIKey:                 vc.APIKey,
		Model:                  vc.Model,
		Timeout:                fmt.Sprintf("%ds", sec),
		Detail:                 vc.Detail,
		DisableThinking:        vc.DisableThinking,
		AnalyzeFrames:          vc.AnalyzeFrames,
		AnalyzeIntervalSeconds: vc.AnalyzeIntervalSeconds,
		MinConfidence:          vc.MinConfidence,
		FailureStreak:          vc.FailureStreak,
		CooldownSeconds:        vc.CooldownSeconds,
		Webhook:                webhookYAML{Enabled: vc.Webhook.Enabled, URL: vc.Webhook.URL},
		Bark: barkYAML{
			Enabled: vc.Bark.Enabled, Key: vc.Bark.Key, Group: vc.Bark.Group,
			Level: vc.Bark.Level, Volume: vc.Bark.Volume, BaseURL: vc.Bark.BaseURL,
		},
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(struct {
		Vision visionYAML `yaml:"vision"`
	}{Vision: v}); err != nil {
		return "", err
	}
	_ = enc.Close()
	return strings.TrimRight(buf.String(), "\n"), nil
}

// isTopLevel 判断是否为顶层级配置行（行首非空白、非注释）。
func isTopLevel(line string) bool {
	t := strings.TrimSpace(line)
	return t != "" && !strings.HasPrefix(t, "#") && line[0] != ' ' && line[0] != '\t'
}

func indentLine(old, content string) string {
	indent := ""
	for _, c := range old {
		if c == ' ' || c == '\t' {
			indent += string(c)
		} else {
			break
		}
	}
	return indent + content
}

// upsertTopLevel 用新块（首行即顶层级 key）替换旧块；不存在则追加到文件末尾。
func upsertTopLevel(lines []string, block string) []string {
	key := block[:strings.Index(block, ":")]
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == key+":" {
			start = i
			break
		}
	}
	if start < 0 {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		return append(lines, strings.Split(block, "\n")...)
	}
	end := start + 1
	for end < len(lines) && !isTopLevel(lines[end]) {
		end++
	}
	// 去掉被替换块末尾紧邻的空行（保留一个分隔空行）
	repl := append([]string{}, strings.Split(block, "\n")...)
	out := append([]string{}, lines[:start]...)
	out = append(out, repl...)
	if end < len(lines) {
		out = append(out, lines[end:]...)
	}
	return out
}

// othersRaw 返回配置文件中除 vision 段外的原文（设置页“其他设置”用，避免 Key 回传）。
func (s *Server) othersRaw() string {
	raw, err := os.ReadFile(s.cfgPath)
	if err != nil {
		return ""
	}
	lines := splitLines(string(raw))
	start, end := findTopBlock(lines, "vision")
	if start < 0 {
		return strings.Join(lines, "\n")
	}
	out := append([]string{}, lines[:start]...)
	out = append(out, lines[end:]...)
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// applyOthersConfig 用页面提交的“其它配置”YAML + 自动合并 vision 段，写回 config 文件。
func (s *Server) applyOthersConfig(raw []byte, in configInput) error {
	others := strings.TrimSpace(*in.Others)
	if others == "" {
		return fmt.Errorf("其它配置不能为空")
	}
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(others), &node); err != nil {
		return fmt.Errorf("YAML 语法错误: %w", err)
	}
	lines := splitLines(others)
	lines = removeTopBlock(lines, "vision") // 即使误贴了 vision 段也剥掉，避免重复

	if in.CaptureMode != nil {
		if err := patchQuickCaptureMode(lines, *in.CaptureMode); err != nil {
			return err
		}
	}

	// vision 段用当前运行配置重建，Key 只取文件里已存的值（不留环境变量 Key）
	stored := storedKeys(raw)
	vc := s.cfg.Vision
	vc.APIKey = stored.visionKey
	vc.Bark.Key = stored.barkKey
	block, err := marshalVision(vc)
	if err != nil {
		return err
	}
	lines = upsertTopLevel(lines, block)

	path := s.cfgPath
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return fmt.Errorf("write config tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// patchQuickCaptureMode 只替换 quick.captureMode 那一行；quick 段不存在则追加。
func patchQuickCaptureMode(lines []string, mode string) error {
	switch strings.TrimSpace(mode) {
	case config.CaptureModeInterval, config.CaptureModeLayer, config.CaptureModeTimestamp:
	default:
		return fmt.Errorf("captureMode 只能是 interval/layer/timestamp")
	}
	mode = strings.TrimSpace(mode)
	replaced := false
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "quick:") {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if isTopLevel(lines[j]) {
				break
			}
			if strings.HasPrefix(strings.TrimSpace(lines[j]), "captureMode:") {
				lines[j] = indentLine(lines[j], fmt.Sprintf("captureMode: %q", mode))
				replaced = true
			}
		}
		break
	}
	if !replaced {
		lines = append(lines, "quick:", fmt.Sprintf("  captureMode: %q", mode))
	}
	return nil
}

func splitLines(s string) []string {
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

// findTopBlock 找顶层 key 块 [start, end)：start 是 "key:" 行，end 是其后第一个顶层级行。
func findTopBlock(lines []string, key string) (int, int) {
	for i, l := range lines {
		if strings.TrimSpace(l) == key+":" {
			end := i + 1
			for end < len(lines) && !isTopLevel(lines[end]) {
				end++
			}
			return i, end
		}
	}
	return -1, -1
}

// removeTopBlock 去掉顶层 key 块（连同块后多余空行）。
func removeTopBlock(lines []string, key string) []string {
	start, end := findTopBlock(lines, key)
	if start < 0 {
		return lines
	}
	out := append([]string{}, lines[:start]...)
	out = append(out, lines[end:]...)
	// 压缩成最多一个分隔空行
	trimmed := make([]string, 0, len(out))
	blank := 0
	for _, l := range out {
		if strings.TrimSpace(l) == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		trimmed = append(trimmed, l)
	}
	for len(trimmed) > 0 && strings.TrimSpace(trimmed[len(trimmed)-1]) == "" {
		trimmed = trimmed[:len(trimmed)-1]
	}
	return trimmed
}
