package ffmpeg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"timelapse/config"
)

// Service 封装所有 FFmpeg/FFprobe 调用。
// Go 侧不做任何 H.264 解码，脏活全部交给 FFmpeg。
type Service struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Service {
	return &Service{cfg: cfg}
}

// StreamInfo 是 ffprobe 探测到的视频流信息。
type StreamInfo struct {
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	CodecName   string `json:"codecName"`
	AvgFrameRate string `json:"avgFrameRate"`
}

// BuildRTSPURL 把用户名密码拼进 RTSP URL（自动 URL 转义）。
func BuildRTSPURL(raw, username, password string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if username != "" {
		if password != "" {
			u.User = url.UserPassword(username, password)
		} else {
			u.User = url.User(username)
		}
	}
	return u.String()
}

// Probe 用 ffprobe 探测 RTSP 流，返回首个视频流信息。用于摄像头测试连接与在线检测。
func (s *Service) Probe(ctx context.Context, input string, timeout time.Duration) (*StreamInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{
		"-v", "error",
		"-rtsp_transport", s.cfg.FFmpeg.RTSPTransport,
		"-timeout", "5000000", // 单位微秒，即 5s 的 socket 超时
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height,codec_name,avg_frame_rate",
		"-of", "json",
		input,
	}
	cmd := exec.CommandContext(ctx, s.cfg.FFmpeg.FFProbe, args...)
	out, err := cmd.Output()
	if err != nil {
		msg := ""
		if ee, ok := err.(*exec.ExitError); ok {
			msg = string(ee.Stderr)
		}
		return nil, fmt.Errorf("probe failed: %v %s", err, msg)
	}

	var res struct {
		Streams []StreamInfo `json:"streams"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("parse probe result: %w", err)
	}
	if len(res.Streams) == 0 {
		return nil, errors.New("no video stream found")
	}
	return &res.Streams[0], nil
}

// CaptureOptions 是抽帧进程的配置。
type CaptureOptions struct {
	IntervalSeconds float64 // fps=1/interval，例如 10 表示每 10 秒一帧
	JPEGQuality     int     // 2-31，越小质量越高
	RTSPTransport   string
}

// CaptureProc 代表一个正在运行的 ffmpeg 抽帧进程。
type CaptureProc struct {
	cmd    *exec.Cmd
	done   chan struct{}
	errMu  sync.Mutex
	errVal error
}

// StartCapture 启动 RTSP → JPEG 抽帧进程（不阻塞）。
// outputPattern 例如 data/frames/task-1/%06d.jpg。
// startNumber 用于断线重连后继续编号，避免覆盖/重复。
func (s *Service) StartCapture(ctx context.Context, input, outputPattern string, startNumber int, opts CaptureOptions, stderr io.Writer) (*CaptureProc, error) {
	if opts.JPEGQuality <= 0 {
		opts.JPEGQuality = s.cfg.FFmpeg.CaptureJPEGQuality
	}
	if opts.RTSPTransport == "" {
		opts.RTSPTransport = s.cfg.FFmpeg.RTSPTransport
	}

	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-rtsp_transport", opts.RTSPTransport,
		"-use_wallclock_as_timestamps", "1",
		"-i", input,
		"-an",
		"-vf", fmt.Sprintf("fps=1/%g", opts.IntervalSeconds),
		"-q:v", strconv.Itoa(opts.JPEGQuality),
		"-start_number", strconv.Itoa(startNumber),
		"-y",
		outputPattern,
	}

	cmd := exec.CommandContext(ctx, s.cfg.FFmpeg.Binary, args...)
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg capture: %w", err)
	}

	p := &CaptureProc{cmd: cmd, done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		p.errMu.Lock()
		p.errVal = err
		p.errMu.Unlock()
		close(p.done)
	}()
	return p, nil
}

// Done 在进程退出时关闭。
func (p *CaptureProc) Done() <-chan struct{} { return p.done }

// Err 返回进程退出错误（nil 表示正常退出）。
func (p *CaptureProc) Err() error {
	p.errMu.Lock()
	defer p.errMu.Unlock()
	return p.errVal
}

// Signal 给进程发送信号（例如 SIGTERM 优雅停止）。
func (p *CaptureProc) Signal(sig os.Signal) error {
	if p.cmd.Process == nil {
		return errors.New("process not started")
	}
	return p.cmd.Process.Signal(sig)
}

// Kill 强制结束进程。
func (p *CaptureProc) Kill() error {
	if p.cmd.Process == nil {
		return errors.New("process not started")
	}
	return p.cmd.Process.Kill()
}

// Encode 把一组 JPEG 帧编码为 H.264 MP4。
// inputPattern 例如 data/frames/task-1/%06d.jpg。
func (s *Service) Encode(ctx context.Context, inputPattern, output string, fps, width, height int, preset string, crf int, stderr io.Writer) error {
	// yuv420p 要求宽高为偶数，超出的像素用 pad 补齐。
	w, h := width, height
	if w%2 != 0 {
		w++
	}
	if h%2 != 0 {
		h++
	}

	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-framerate", strconv.Itoa(fps),
		"-i", inputPattern,
		"-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2", w, h, w, h),
		"-c:v", "libx264",
		"-preset", preset,
		"-crf", strconv.Itoa(crf),
	}
	// 限制编码线程数：0 表示自动（默认吃满所有核）；小盒子可设 2-3 降低 CPU 占用
	if threads := s.cfg.FFmpeg.EncodeThreads; threads > 0 {
		args = append(args, "-threads", strconv.Itoa(threads))
	}
	// 码率上限防止画面突变时体积暴涨；bufsize 取 2 倍上限
	if maxRate := s.cfg.FFmpeg.EncodeMaxRateKbps; maxRate > 0 {
		args = append(args, "-maxrate", fmt.Sprintf("%dk", maxRate), "-bufsize", fmt.Sprintf("%dk", 2*maxRate))
	}
	args = append(args,
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		output,
	)

	cmd := exec.CommandContext(ctx, s.cfg.FFmpeg.Binary, args...)
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("encode failed: %w", err)
	}
	return nil
}
