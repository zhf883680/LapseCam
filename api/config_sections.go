package api

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"timelapse/config"
)

// ---- 视图（页面回填用，JSON 友好，密钥不在此）----

type serverView struct {
	Addr string `json:"addr"`
}

type databaseView struct {
	Path string `json:"path"`
}

type storageView struct {
	BaseDir   string `json:"baseDir"`
	FramesDir string `json:"framesDir"`
	VideosDir string `json:"videosDir"`
}

type ffmpegView struct {
	Binary             string   `json:"binary"`
	FFProbe            string   `json:"ffprobe"`
	ProbeTimeoutSec    int      `json:"probeTimeoutSec"`
	RTSPTransport      string   `json:"rtspTransport"`
	CaptureJPEGQuality int      `json:"captureJPEGQuality"`
	CaptureBackoff     []string `json:"captureBackoff"`
	EncodePreset       string   `json:"encodePreset"`
	EncodeCRF          int      `json:"encodeCRF"`
	EncodeMaxRateKbps  int      `json:"encodeMaxRateKbps"`
	EncodeThreads      int      `json:"encodeThreads"`
}

type previewView struct {
	Enabled         bool   `json:"enabled"`
	Binary          string `json:"binary"`
	Addr            string `json:"addr"`
	BasePath        string `json:"basePath"`
	RTSPTransport   string `json:"rtspTransport"`
	Media           string `json:"media"`
	StartTimeoutSec int    `json:"startTimeoutSec"`
}

type schedulerView struct {
	TickSeconds        int `json:"tickSeconds"`
	CameraCheckSeconds int `json:"cameraCheckSeconds"`
}

type quickView struct {
	Name               string  `json:"name"`
	CaptureMode        string  `json:"captureMode"`
	IntervalSeconds    int     `json:"intervalSeconds"`
	LayerOffsetSeconds float64 `json:"layerOffsetSeconds"`
	LayerWindowSeconds float64 `json:"layerWindowSeconds"`
	OutputFPS          int     `json:"outputFps"`
	Width              int     `json:"width"`
	Height             int     `json:"height"`
}

type cleanupView struct {
	Enabled                 bool `json:"enabled"`
	IntervalHours           int  `json:"intervalHours"`
	RemoveFramesAfterEncode bool `json:"removeFramesAfterEncode"`
	VideoRetentionDays      int  `json:"videoRetentionDays"`
	RemoveOrphans           bool `json:"removeOrphans"`
}

// ---- 入参（指针，缺省 = 不修改）----

type serverIn struct {
	Addr *string `json:"addr"`
}

type databaseIn struct {
	Path *string `json:"path"`
}

type storageIn struct {
	BaseDir   *string `json:"baseDir"`
	FramesDir *string `json:"framesDir"`
	VideosDir *string `json:"videosDir"`
}

type ffmpegIn struct {
	Binary             *string   `json:"binary"`
	FFProbe            *string   `json:"ffprobe"`
	ProbeTimeoutSec    *int      `json:"probeTimeoutSec"`
	RTSPTransport      *string   `json:"rtspTransport"`
	CaptureJPEGQuality *int      `json:"captureJPEGQuality"`
	CaptureBackoff     *[]string `json:"captureBackoff"`
	EncodePreset       *string   `json:"encodePreset"`
	EncodeCRF          *int      `json:"encodeCRF"`
	EncodeMaxRateKbps  *int      `json:"encodeMaxRateKbps"`
	EncodeThreads      *int      `json:"encodeThreads"`
}

type previewIn struct {
	Enabled         *bool   `json:"enabled"`
	Binary          *string `json:"binary"`
	Addr            *string `json:"addr"`
	BasePath        *string `json:"basePath"`
	RTSPTransport   *string `json:"rtspTransport"`
	Media           *string `json:"media"`
	StartTimeoutSec *int    `json:"startTimeoutSec"`
}

type schedulerIn struct {
	TickSeconds        *int `json:"tickSeconds"`
	CameraCheckSeconds *int `json:"cameraCheckSeconds"`
}

type quickIn struct {
	Name               *string  `json:"name"`
	CaptureMode        *string  `json:"captureMode"`
	IntervalSeconds    *int     `json:"intervalSeconds"`
	LayerOffsetSeconds *float64 `json:"layerOffsetSeconds"`
	LayerWindowSeconds *float64 `json:"layerWindowSeconds"`
	OutputFPS          *int     `json:"outputFps"`
	Width              *int     `json:"width"`
	Height             *int     `json:"height"`
}

type cleanupIn struct {
	Enabled                 *bool `json:"enabled"`
	IntervalHours           *int  `json:"intervalHours"`
	RemoveFramesAfterEncode *bool `json:"removeFramesAfterEncode"`
	VideoRetentionDays      *int  `json:"videoRetentionDays"`
	RemoveOrphans           *bool `json:"removeOrphans"`
}

// ---- 从运行配置构造视图 ----

func sectionServer(c *config.Config) serverView     { return serverView{Addr: c.Server.Addr} }
func sectionDatabase(c *config.Config) databaseView { return databaseView{Path: c.Database.Path} }
func sectionStorage(c *config.Config) storageView {
	return storageView{BaseDir: c.Storage.BaseDir, FramesDir: c.Storage.FramesDir, VideosDir: c.Storage.VideosDir}
}
func sectionFFmpeg(c *config.Config) ffmpegView {
	return ffmpegView{
		Binary: c.FFmpeg.Binary, FFProbe: c.FFmpeg.FFProbe,
		ProbeTimeoutSec: int(c.FFmpeg.ProbeTimeout / time.Second),
		RTSPTransport:   c.FFmpeg.RTSPTransport, CaptureJPEGQuality: c.FFmpeg.CaptureJPEGQuality,
		CaptureBackoff: durSlice(c.FFmpeg.CaptureBackoff), EncodePreset: c.FFmpeg.EncodePreset,
		EncodeCRF: c.FFmpeg.EncodeCRF, EncodeMaxRateKbps: c.FFmpeg.EncodeMaxRateKbps,
		EncodeThreads: c.FFmpeg.EncodeThreads,
	}
}
func sectionPreview(c *config.Config) previewView {
	return previewView{
		Enabled: c.Preview.Enabled, Binary: c.Preview.Binary, Addr: c.Preview.Addr,
		BasePath: c.Preview.BasePath, RTSPTransport: c.Preview.RTSPTransport,
		Media: c.Preview.Media, StartTimeoutSec: int(c.Preview.StartTimeout / time.Second),
	}
}
func sectionScheduler(c *config.Config) schedulerView {
	return schedulerView{TickSeconds: c.Scheduler.TickSeconds, CameraCheckSeconds: c.Scheduler.CameraCheckSeconds}
}
func sectionQuick(c *config.Config) quickView {
	return quickView{
		Name: c.Quick.Name, CaptureMode: c.Quick.CaptureMode, IntervalSeconds: c.Quick.IntervalSeconds,
		LayerOffsetSeconds: c.Quick.LayerOffsetSeconds, LayerWindowSeconds: c.Quick.LayerWindowSeconds,
		OutputFPS: c.Quick.OutputFPS, Width: c.Quick.Width, Height: c.Quick.Height,
	}
}
func sectionCleanup(c *config.Config) cleanupView {
	return cleanupView{
		Enabled: c.Cleanup.Enabled, IntervalHours: c.Cleanup.IntervalHours,
		RemoveFramesAfterEncode: c.Cleanup.RemoveFramesAfterEncode,
		VideoRetentionDays:      c.Cleanup.VideoRetentionDays, RemoveOrphans: c.Cleanup.RemoveOrphans,
	}
}

func durSlice(vals []time.Duration) []string {
	out := make([]string, 0, len(vals))
	for _, d := range vals {
		out = append(out, d.String())
	}
	return out
}

// ---- 把入参合并进运行配置（就地修改 cfg，便于序列化）----

func applySections(cfg *config.Config, in configInput) {
	if in.Server != nil {
		if in.Server.Addr != nil {
			cfg.Server.Addr = *in.Server.Addr
		}
	}
	if in.Database != nil {
		if in.Database.Path != nil {
			cfg.Database.Path = *in.Database.Path
		}
	}
	if in.Storage != nil {
		if in.Storage.BaseDir != nil {
			cfg.Storage.BaseDir = *in.Storage.BaseDir
		}
		if in.Storage.FramesDir != nil {
			cfg.Storage.FramesDir = *in.Storage.FramesDir
		}
		if in.Storage.VideosDir != nil {
			cfg.Storage.VideosDir = *in.Storage.VideosDir
		}
	}
	if in.FFmpeg != nil {
		if in.FFmpeg.Binary != nil {
			cfg.FFmpeg.Binary = *in.FFmpeg.Binary
		}
		if in.FFmpeg.FFProbe != nil {
			cfg.FFmpeg.FFProbe = *in.FFmpeg.FFProbe
		}
		if in.FFmpeg.ProbeTimeoutSec != nil && *in.FFmpeg.ProbeTimeoutSec > 0 {
			cfg.FFmpeg.ProbeTimeout = time.Duration(*in.FFmpeg.ProbeTimeoutSec) * time.Second
		}
		if in.FFmpeg.RTSPTransport != nil {
			cfg.FFmpeg.RTSPTransport = *in.FFmpeg.RTSPTransport
		}
		if in.FFmpeg.CaptureJPEGQuality != nil {
			cfg.FFmpeg.CaptureJPEGQuality = *in.FFmpeg.CaptureJPEGQuality
		}
		if in.FFmpeg.CaptureBackoff != nil {
			vals := make([]time.Duration, 0, len(*in.FFmpeg.CaptureBackoff))
			for _, s := range *in.FFmpeg.CaptureBackoff {
				if d, err := parseDur(s); err == nil {
					vals = append(vals, d)
				}
			}
			cfg.FFmpeg.CaptureBackoff = vals
		}
		if in.FFmpeg.EncodePreset != nil {
			cfg.FFmpeg.EncodePreset = *in.FFmpeg.EncodePreset
		}
		if in.FFmpeg.EncodeCRF != nil {
			cfg.FFmpeg.EncodeCRF = *in.FFmpeg.EncodeCRF
		}
		if in.FFmpeg.EncodeMaxRateKbps != nil {
			cfg.FFmpeg.EncodeMaxRateKbps = *in.FFmpeg.EncodeMaxRateKbps
		}
		if in.FFmpeg.EncodeThreads != nil {
			cfg.FFmpeg.EncodeThreads = *in.FFmpeg.EncodeThreads
		}
	}
	if in.Preview != nil {
		if in.Preview.Enabled != nil {
			cfg.Preview.Enabled = *in.Preview.Enabled
		}
		if in.Preview.Binary != nil {
			cfg.Preview.Binary = *in.Preview.Binary
		}
		if in.Preview.Addr != nil {
			cfg.Preview.Addr = *in.Preview.Addr
		}
		if in.Preview.BasePath != nil {
			cfg.Preview.BasePath = *in.Preview.BasePath
		}
		if in.Preview.RTSPTransport != nil {
			cfg.Preview.RTSPTransport = *in.Preview.RTSPTransport
		}
		if in.Preview.Media != nil {
			cfg.Preview.Media = *in.Preview.Media
		}
		if in.Preview.StartTimeoutSec != nil && *in.Preview.StartTimeoutSec > 0 {
			cfg.Preview.StartTimeout = time.Duration(*in.Preview.StartTimeoutSec) * time.Second
		}
	}
	if in.Scheduler != nil {
		if in.Scheduler.TickSeconds != nil {
			cfg.Scheduler.TickSeconds = *in.Scheduler.TickSeconds
		}
		if in.Scheduler.CameraCheckSeconds != nil {
			cfg.Scheduler.CameraCheckSeconds = *in.Scheduler.CameraCheckSeconds
		}
	}
	if in.Quick != nil {
		if in.Quick.Name != nil {
			cfg.Quick.Name = *in.Quick.Name
		}
		if in.Quick.CaptureMode != nil {
			cfg.Quick.CaptureMode = *in.Quick.CaptureMode
		}
		if in.Quick.IntervalSeconds != nil {
			cfg.Quick.IntervalSeconds = *in.Quick.IntervalSeconds
		}
		if in.Quick.LayerOffsetSeconds != nil {
			cfg.Quick.LayerOffsetSeconds = *in.Quick.LayerOffsetSeconds
		}
		if in.Quick.LayerWindowSeconds != nil {
			cfg.Quick.LayerWindowSeconds = *in.Quick.LayerWindowSeconds
		}
		if in.Quick.OutputFPS != nil {
			cfg.Quick.OutputFPS = *in.Quick.OutputFPS
		}
		if in.Quick.Width != nil {
			cfg.Quick.Width = *in.Quick.Width
		}
		if in.Quick.Height != nil {
			cfg.Quick.Height = *in.Quick.Height
		}
	}
	if in.Cleanup != nil {
		if in.Cleanup.Enabled != nil {
			cfg.Cleanup.Enabled = *in.Cleanup.Enabled
		}
		if in.Cleanup.IntervalHours != nil {
			cfg.Cleanup.IntervalHours = *in.Cleanup.IntervalHours
		}
		if in.Cleanup.RemoveFramesAfterEncode != nil {
			cfg.Cleanup.RemoveFramesAfterEncode = *in.Cleanup.RemoveFramesAfterEncode
		}
		if in.Cleanup.VideoRetentionDays != nil {
			cfg.Cleanup.VideoRetentionDays = *in.Cleanup.VideoRetentionDays
		}
		if in.Cleanup.RemoveOrphans != nil {
			cfg.Cleanup.RemoveOrphans = *in.Cleanup.RemoveOrphans
		}
	}
}

func parseDur(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	return time.ParseDuration(s)
}

// ---- 把每个配置段序列化成 YAML 块（不覆盖注释，段级替换）----

func sectionBlock(key string, v any) (string, error) {
	b, err := yaml.Marshal(v)
	if err != nil {
		return "", err
	}
	body := strings.TrimRight(string(b), "\n")
	lines := strings.Split(body, "\n")
	out := []string{key + ":"}
	for _, l := range lines {
		if l == "" {
			out = append(out, l)
		} else {
			out = append(out, "  "+l)
		}
	}
	return strings.Join(out, "\n"), nil
}

type serverYAML struct {
	Addr string `yaml:"addr"`
}
type databaseYAML struct {
	Path string `yaml:"path"`
}
type storageYAML struct {
	BaseDir   string `yaml:"baseDir"`
	FramesDir string `yaml:"framesDir"`
	VideosDir string `yaml:"videosDir"`
}
type ffmpegYAML struct {
	Binary             string   `yaml:"binary"`
	FFProbe            string   `yaml:"ffprobe"`
	ProbeTimeout       string   `yaml:"probeTimeout"`
	RTSPTransport      string   `yaml:"rtspTransport"`
	CaptureJPEGQuality int      `yaml:"captureJPEGQuality"`
	CaptureBackoff     []string `yaml:"captureBackoff"`
	EncodePreset       string   `yaml:"encodePreset"`
	EncodeCRF          int      `yaml:"encodeCRF"`
	EncodeMaxRateKbps  int      `yaml:"encodeMaxRateKbps"`
	EncodeThreads      int      `yaml:"encodeThreads"`
}
type previewYAML struct {
	Enabled       bool   `yaml:"enabled"`
	Binary        string `yaml:"binary"`
	Addr          string `yaml:"addr"`
	BasePath      string `yaml:"basePath"`
	RTSPTransport string `yaml:"rtspTransport"`
	Media         string `yaml:"media"`
	StartTimeout  string `yaml:"startTimeout"`
}
type schedulerYAML struct {
	TickSeconds        int `yaml:"tickSeconds"`
	CameraCheckSeconds int `yaml:"cameraCheckSeconds"`
}
type quickYAML struct {
	Name               string  `yaml:"name"`
	CaptureMode        string  `yaml:"captureMode"`
	IntervalSeconds    int     `yaml:"intervalSeconds"`
	LayerOffsetSeconds float64 `yaml:"layerOffsetSeconds"`
	LayerWindowSeconds float64 `yaml:"layerWindowSeconds"`
	OutputFPS          int     `yaml:"outputFps"`
	Width              int     `yaml:"width"`
	Height             int     `yaml:"height"`
}
type cleanupYAML struct {
	Enabled                 bool `yaml:"enabled"`
	IntervalHours           int  `yaml:"intervalHours"`
	RemoveFramesAfterEncode bool `yaml:"removeFramesAfterEncode"`
	VideoRetentionDays      int  `yaml:"videoRetentionDays"`
	RemoveOrphans           bool `yaml:"removeOrphans"`
}

func marshalServer(c config.ServerConfig) (string, error) {
	return sectionBlock("server", serverYAML{Addr: c.Addr})
}
func marshalDatabase(c config.DatabaseConfig) (string, error) {
	return sectionBlock("database", databaseYAML{Path: c.Path})
}
func marshalStorage(c config.StorageConfig) (string, error) {
	return sectionBlock("storage", storageYAML{BaseDir: c.BaseDir, FramesDir: c.FramesDir, VideosDir: c.VideosDir})
}
func marshalFFmpeg(c config.FFmpegConfig) (string, error) {
	return sectionBlock("ffmpeg", ffmpegYAML{
		Binary: c.Binary, FFProbe: c.FFProbe, ProbeTimeout: c.ProbeTimeout.String(),
		RTSPTransport: c.RTSPTransport, CaptureJPEGQuality: c.CaptureJPEGQuality,
		CaptureBackoff: durSlice(c.CaptureBackoff), EncodePreset: c.EncodePreset,
		EncodeCRF: c.EncodeCRF, EncodeMaxRateKbps: c.EncodeMaxRateKbps, EncodeThreads: c.EncodeThreads,
	})
}
func marshalPreview(c config.PreviewConfig) (string, error) {
	return sectionBlock("preview", previewYAML{
		Enabled: c.Enabled, Binary: c.Binary, Addr: c.Addr, BasePath: c.BasePath,
		RTSPTransport: c.RTSPTransport, Media: c.Media, StartTimeout: c.StartTimeout.String(),
	})
}
func marshalScheduler(c config.SchedulerConfig) (string, error) {
	return sectionBlock("scheduler", schedulerYAML{TickSeconds: c.TickSeconds, CameraCheckSeconds: c.CameraCheckSeconds})
}
func marshalQuick(c config.QuickConfig) (string, error) {
	return sectionBlock("quick", quickYAML{
		Name: c.Name, CaptureMode: c.CaptureMode, IntervalSeconds: c.IntervalSeconds,
		LayerOffsetSeconds: c.LayerOffsetSeconds, LayerWindowSeconds: c.LayerWindowSeconds,
		OutputFPS: c.OutputFPS, Width: c.Width, Height: c.Height,
	})
}
func marshalCleanup(c config.CleanupConfig) (string, error) {
	return sectionBlock("cleanup", cleanupYAML{
		Enabled: c.Enabled, IntervalHours: c.IntervalHours,
		RemoveFramesAfterEncode: c.RemoveFramesAfterEncode,
		VideoRetentionDays:      c.VideoRetentionDays, RemoveOrphans: c.RemoveOrphans,
	})
}
