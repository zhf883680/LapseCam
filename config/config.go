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
	Scheduler SchedulerConfig `yaml:"scheduler"`
	Quick     QuickConfig     `yaml:"quick"`
}

// QuickConfig 快捷录制配置（POST /api/quick/start 与 /api/quick/stop）。
type QuickConfig struct {
	Name            string `yaml:"name"`            // 快捷任务名称（用于识别）
	IntervalSeconds int    `yaml:"intervalSeconds"` // 抽帧间隔（秒）
	OutputFPS       int    `yaml:"outputFps"`       // 成片帧率
	Width           int    `yaml:"width"`           // 成片宽
	Height          int    `yaml:"height"`          // 成片高
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
			EncodePreset:       "slow",
			EncodeCRF:          26,
			EncodeMaxRateKbps:  4000,
		},
		Scheduler: SchedulerConfig{TickSeconds: 1, CameraCheckSeconds: 60},
		Quick: QuickConfig{
			Name:            "快捷录制",
			IntervalSeconds: 5,
			OutputFPS:       30,
			Width:           1280,
			Height:          720,
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
	if c.Scheduler.TickSeconds <= 0 {
		c.Scheduler.TickSeconds = d.Scheduler.TickSeconds
	}
	if c.Scheduler.CameraCheckSeconds < 0 {
		c.Scheduler.CameraCheckSeconds = d.Scheduler.CameraCheckSeconds
	}
	if c.Quick.Name == "" {
		c.Quick.Name = d.Quick.Name
	}
	if c.Quick.IntervalSeconds <= 0 {
		c.Quick.IntervalSeconds = d.Quick.IntervalSeconds
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
}

// FramesBaseDir 返回抽帧根目录（绝对路径由 Storage 层拼接）。
func (c *Config) FramesBaseDir() string { return c.Storage.BaseDir + "/" + c.Storage.FramesDir }

// VideosBaseDir 返回视频根目录。
func (c *Config) VideosBaseDir() string { return c.Storage.BaseDir + "/" + c.Storage.VideosDir }
