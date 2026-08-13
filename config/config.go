package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 是服务整体配置，对应 config/config.yaml。
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	Storage   StorageConfig   `yaml:"storage"`
	FFmpeg    FFmpegConfig    `yaml:"ffmpeg"`
	Preview   PreviewConfig   `yaml:"preview"`
	Scheduler SchedulerConfig `yaml:"scheduler"`
	Quick     QuickConfig     `yaml:"quick"`
	Cleanup   CleanupConfig   `yaml:"cleanup"`
}

// CleanupConfig 数据清理（释放磁盘空间）。
type CleanupConfig struct {
	Enabled                 bool `yaml:"enabled"`                 // 自动清理总开关（手动清理不受此开关控制）
	IntervalHours           int  `yaml:"intervalHours"`           // 自动清理间隔（小时），<=0 回退默认 24
	RemoveFramesAfterEncode bool `yaml:"removeFramesAfterEncode"` // 出片成功后删除中间帧（含 selected/ 与 layers 标记）
	VideoRetentionDays      int  `yaml:"videoRetentionDays"`      // 0=保留全部；>0 只保留最近 N 天视频记录及文件
	RemoveOrphans           bool `yaml:"removeOrphans"`           // 清理孤儿数据（无主 task-{id} 目录/日志/标记）
}

// 快捷录制抽帧模式
const (
	CaptureModeInterval  = "interval"  // 定时抽帧（默认，原有行为）
	CaptureModeLayer     = "layer"     // 逐层截图：外部每层触发 POST /api/quick/snapshot
	CaptureModeTimestamp = "timestamp" // 逐层选帧：连续抽帧 + 记录层变化时间戳，出片时挑最接近每层的帧
)

// QuickConfig 快捷录制配置（POST /api/quick/start 与 /api/quick/stop）。
type QuickConfig struct {
	Name               string  `yaml:"name"`            // 快捷任务名称（用于识别）
	CaptureMode        string  `yaml:"captureMode"`     // 抽帧模式：interval | layer | timestamp
	IntervalSeconds    int     `yaml:"intervalSeconds"` // 抽帧间隔（秒），interval/timestamp 模式使用
	LayerOffsetSeconds float64 `yaml:"layerOffsetSeconds"` // timestamp 模式：选帧目标时刻 = 层变化时刻 + 偏移（秒，可负）
	LayerWindowSeconds float64 `yaml:"layerWindowSeconds"` // timestamp 模式：选帧窗口（秒），窗口内取最接近的一帧
	OutputFPS          int     `yaml:"outputFps"`       // 成片帧率
	Width              int     `yaml:"width"`           // 成片宽
	Height             int     `yaml:"height"`          // 成片高
}

type ServerConfig struct {
	Addr string `yaml:"addr"` // 监听地址，如 :8080
}

type DatabaseConfig struct {
	Path string `yaml:"path"` // SQLite 文件路径
}

type StorageConfig struct {
	BaseDir   string `yaml:"baseDir"`   // 数据根目录
	FramesDir string `yaml:"framesDir"` // 抽帧图片目录（相对 BaseDir）
	VideosDir string `yaml:"videosDir"` // 视频目录（相对 BaseDir）
}

type FFmpegConfig struct {
	Binary             string          `yaml:"binary"` // ffmpeg 可执行文件
	FFProbe            string          `yaml:"ffprobe"`
	ProbeTimeout       time.Duration   `yaml:"probeTimeout"` // 探测摄像头超时
	RTSPTransport      string          `yaml:"rtspTransport"`
	CaptureJPEGQuality int             `yaml:"captureJPEGQuality"`
	CaptureBackoff     []time.Duration `yaml:"captureBackoff"` // 断线重连退避序列
	EncodePreset       string          `yaml:"encodePreset"`
	EncodeCRF          int             `yaml:"encodeCRF"`
	EncodeMaxRateKbps  int             `yaml:"encodeMaxRateKbps"` // 成片码率上限 kbps（0 或缺省用默认值）
	EncodeThreads      int             `yaml:"encodeThreads"`     // 编码线程数，0 表示自动（小盒子可设 2-3 限制 CPU）
}

// PreviewConfig Web 实时预览（go2rtc）。
// go2rtc 把摄像头 RTSP 流转成浏览器可直接播放的 MSE/HLS，
// 由本服务反向代理 /go2rtc/* 对外提供，保持单端口访问。
type PreviewConfig struct {
	Enabled       bool          `yaml:"enabled"`       // 是否启用预览
	Binary        string        `yaml:"binary"`        // go2rtc 可执行文件路径
	Addr          string        `yaml:"addr"`          // go2rtc 内部监听地址（默认只绑 127.0.0.1，不直接对外）
	BasePath      string        `yaml:"basePath"`      // 反向代理前缀，对外 URL 为 {basePath}/stream.html?src=cam-{id}
	RTSPTransport string        `yaml:"rtspTransport"` // 拉流传输协议：tcp / udp
	Media         string        `yaml:"media"`         // 只取视频轨：video（默认）
	StartTimeout  time.Duration `yaml:"startTimeout"`  // 启动等待 go2rtc 就绪的超时
}

type SchedulerConfig struct {
	TickSeconds        int `yaml:"tickSeconds"`        // 任务调度轮询间隔
	CameraCheckSeconds int `yaml:"cameraCheckSeconds"` // 摄像头在线状态轮询间隔，0 表示关闭
}

// Load 读取 YAML 配置文件并填充默认值。
func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.applyDefaults()
	return cfg, nil
}

// Default 返回带默认值的配置。
func Default() *Config {
	return &Config{
		Server:   ServerConfig{Addr: ":8080"},
		Database: DatabaseConfig{Path: "data/database.db"},
		Storage:  StorageConfig{BaseDir: "data", FramesDir: "frames", VideosDir: "videos"},
		FFmpeg: FFmpegConfig{
			Binary:             "ffmpeg",
			FFProbe:            "ffprobe",
			ProbeTimeout:       8 * time.Second,
			RTSPTransport:      "tcp",
			CaptureJPEGQuality: 2,
			CaptureBackoff:     []time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second, 60 * time.Second},
			EncodePreset:       "veryfast",
			EncodeCRF:          26,
			EncodeMaxRateKbps:  4000,
			EncodeThreads:      0,
		},
		Preview: PreviewConfig{
			Enabled:       true,
			Binary:        "go2rtc",
			Addr:          "127.0.0.1:1984",
			BasePath:      "/go2rtc",
			RTSPTransport: "tcp",
			Media:         "video",
			StartTimeout:  10 * time.Second,
		},
		Scheduler: SchedulerConfig{TickSeconds: 1, CameraCheckSeconds: 60},
		Quick: QuickConfig{
			Name:               "快捷录制",
			CaptureMode:        CaptureModeInterval,
			IntervalSeconds:    5,
			LayerOffsetSeconds: 0,
			LayerWindowSeconds: 5,
			OutputFPS:          30,
			Width:              1280,
			Height:             720,
		},
		Cleanup: CleanupConfig{
			Enabled:                 true,
			IntervalHours:           24,
			RemoveFramesAfterEncode: true,
			VideoRetentionDays:      0,
			RemoveOrphans:           true,
		},
	}
}

func (c *Config) applyDefaults() {
	d := Default()
	if c.Server.Addr == "" {
		c.Server.Addr = d.Server.Addr
	}
	if c.Database.Path == "" {
		c.Database.Path = d.Database.Path
	}
	if c.Storage.BaseDir == "" {
		c.Storage.BaseDir = d.Storage.BaseDir
	}
	if c.Storage.FramesDir == "" {
		c.Storage.FramesDir = d.Storage.FramesDir
	}
	if c.Storage.VideosDir == "" {
		c.Storage.VideosDir = d.Storage.VideosDir
	}
	if c.FFmpeg.Binary == "" {
		c.FFmpeg.Binary = d.FFmpeg.Binary
	}
	if c.FFmpeg.FFProbe == "" {
		c.FFmpeg.FFProbe = d.FFmpeg.FFProbe
	}
	if c.FFmpeg.ProbeTimeout <= 0 {
		c.FFmpeg.ProbeTimeout = d.FFmpeg.ProbeTimeout
	}
	if c.FFmpeg.RTSPTransport == "" {
		c.FFmpeg.RTSPTransport = d.FFmpeg.RTSPTransport
	}
	if c.FFmpeg.CaptureJPEGQuality <= 0 {
		c.FFmpeg.CaptureJPEGQuality = d.FFmpeg.CaptureJPEGQuality
	}
	if len(c.FFmpeg.CaptureBackoff) == 0 {
		c.FFmpeg.CaptureBackoff = d.FFmpeg.CaptureBackoff
	}
	if c.FFmpeg.EncodePreset == "" {
		c.FFmpeg.EncodePreset = d.FFmpeg.EncodePreset
	}
	if c.FFmpeg.EncodeCRF <= 0 {
		c.FFmpeg.EncodeCRF = d.FFmpeg.EncodeCRF
	}
	if c.FFmpeg.EncodeMaxRateKbps <= 0 {
		c.FFmpeg.EncodeMaxRateKbps = d.FFmpeg.EncodeMaxRateKbps
	}
	if c.FFmpeg.EncodeThreads < 0 {
		c.FFmpeg.EncodeThreads = d.FFmpeg.EncodeThreads
	}
	if c.Preview.Binary == "" {
		c.Preview.Binary = d.Preview.Binary
	}
	if c.Preview.Addr == "" {
		c.Preview.Addr = d.Preview.Addr
	}
	if c.Preview.BasePath == "" {
		c.Preview.BasePath = d.Preview.BasePath
	}
	if c.Preview.RTSPTransport == "" {
		c.Preview.RTSPTransport = d.Preview.RTSPTransport
	}
	if c.Preview.Media == "" {
		c.Preview.Media = d.Preview.Media
	}
	if c.Preview.StartTimeout <= 0 {
		c.Preview.StartTimeout = d.Preview.StartTimeout
	}
	if c.Scheduler.TickSeconds <= 0 {
		c.Scheduler.TickSeconds = d.Scheduler.TickSeconds
	}
	if c.Scheduler.CameraCheckSeconds < 0 {
		c.Scheduler.CameraCheckSeconds = d.Scheduler.CameraCheckSeconds
	}
	if c.Quick.Name == "" {
		c.Quick.Name = d.Quick.Name
	}
	switch c.Quick.CaptureMode {
	case "", CaptureModeInterval, CaptureModeLayer, CaptureModeTimestamp:
		if c.Quick.CaptureMode == "" {
			c.Quick.CaptureMode = d.Quick.CaptureMode
		}
	default:
		c.Quick.CaptureMode = d.Quick.CaptureMode // 非法值回退默认
	}
	if c.Quick.IntervalSeconds <= 0 {
		c.Quick.IntervalSeconds = d.Quick.IntervalSeconds
	}
	if c.Quick.LayerWindowSeconds <= 0 {
		c.Quick.LayerWindowSeconds = d.Quick.LayerWindowSeconds
	}
	if c.Quick.OutputFPS <= 0 {
		c.Quick.OutputFPS = d.Quick.OutputFPS
	}
	if c.Quick.Width <= 0 {
		c.Quick.Width = d.Quick.Width
	}
	if c.Quick.Height <= 0 {
		c.Quick.Height = d.Quick.Height
	}
	if c.Cleanup.IntervalHours <= 0 {
		c.Cleanup.IntervalHours = d.Cleanup.IntervalHours
	}
}

// FramesBaseDir 返回抽帧根目录（绝对路径由 Storage 层拼接）。
func (c *Config) FramesBaseDir() string { return c.Storage.BaseDir + "/" + c.Storage.FramesDir }

// VideosBaseDir 返回视频根目录。
func (c *Config) VideosBaseDir() string { return c.Storage.BaseDir + "/" + c.Storage.VideosDir }
