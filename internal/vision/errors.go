package vision

import "errors"

// ErrNotConfigured AI 视觉分析未配置（vision.enabled=false 或缺 API Key）。
var ErrNotConfigured = errors.New("vision not configured")

// ErrNoImages 没有提供任何图片。
var ErrNoImages = errors.New("no images provided")
