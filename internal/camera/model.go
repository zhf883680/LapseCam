package camera

import "time"

// Camera 摄像头配置。
type Camera struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	RtspURL     string     `json:"rtspUrl"`
	Username    string     `json:"username"`
	Password    string     `json:"-"` // 不回传明文
	HasPassword bool       `json:"hasPassword"`
	Enabled     bool       `json:"enabled"`
	Online      bool       `json:"online"`
	LastSeenAt  *time.Time `json:"lastSeenAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// CameraInput 创建/修改摄像头的入参。
type CameraInput struct {
	Name     string `json:"name"`
	RtspURL  string `json:"rtspUrl"`
	Username string `json:"username"`
	Password string `json:"password"`
	Enabled  *bool  `json:"enabled"`
}

// TestResult 测试连接的结果。
type TestResult struct {
	OK      bool     `json:"ok"`
	Message string   `json:"message"`
	Stream  *Stream  `json:"stream,omitempty"`
}

// Stream 探测到的视频流信息（对外展示用）。
type Stream struct {
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	CodecName   string `json:"codecName"`
	AvgFrameRate string `json:"avgFrameRate"`
}
