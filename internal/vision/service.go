package vision

import (
	"context"
	"log"
	"os"
	"strings"

	"timelapse/config"
)

// Detector 是视觉分析的实现接口。
// 第一版只有 OpenAI 兼容（DeepSeek）实现；以后接 Gemini/Ollama 时再实现一个 Detector 即可。
type Detector interface {
	// Analyze 分析一组图片（按时间从旧到新），返回一个整体结论。
	Analyze(ctx context.Context, images [][]byte) (*AnalysisResult, error)
}

// Service 是 AI 视觉分析的统一入口。
type Service struct {
	cfg *config.Config
	det Detector
}

// New 按配置构造视觉服务。API Key 优先级：配置 vision.apiKey > 环境变量 VISION_API_KEY。
func New(cfg *config.Config) *Service {
	s := &Service{cfg: cfg}
	if !cfg.Vision.Enabled {
		return s
	}
	key := strings.TrimSpace(cfg.Vision.APIKey)
	if key == "" {
		key = strings.TrimSpace(os.Getenv("VISION_API_KEY"))
	}
	if key == "" {
		log.Printf("[vision] vision.enabled=true 但未配置 apiKey 且环境变量 VISION_API_KEY 为空，AI 分析不可用")
		return s
	}
	det := &openAICompatible{
		baseURL:         strings.TrimRight(cfg.Vision.BaseURL, "/"),
		apiKey:          key,
		model:           cfg.Vision.Model,
		detail:          cfg.Vision.Detail,
		maxImageWidth:   cfg.Vision.MaxImageWidth,
		imageSource:     cfg.Vision.ImageSource,
		useTempURL:      cfg.Vision.ImageSource == "temp",
		timeout:         cfg.Vision.Timeout,
		disableThinking: cfg.Vision.DisableThinking,
	}
	if det.useTempURL {
		det.uploader = newTempUploader(det.baseURL, det.apiKey, det.model)
	}
	s.det = det
	return s
}

// Enabled AI 分析是否可用。
func (s *Service) Enabled() bool { return s.det != nil }

// Analyze 分析一组 JPEG/PNG 图片，返回整体结论。AI 不可用返回 ErrNotConfigured。
func (s *Service) Analyze(ctx context.Context, images [][]byte) (*AnalysisResult, error) {
	if s.det == nil {
		return nil, ErrNotConfigured
	}
	if len(images) == 0 {
		return nil, ErrNoImages
	}
	return s.det.Analyze(ctx, images)
}
