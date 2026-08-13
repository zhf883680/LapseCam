package preview

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"timelapse/config"
	"timelapse/internal/camera"
	"timelapse/internal/ffmpeg"
)

// Service 管理 go2rtc 子进程，把摄像头 RTSP 流转成浏览器可直接播放的 MSE/HLS。
//
// 设计：
//   - go2rtc 只绑 127.0.0.1，不直接对外；由 LapseCam 反向代理 {BasePath}/* 到前端，
//     保持单端口访问；
//   - 摄像头增删改后调用 Sync 同步 go2rtc 里的流（动态 REST API，无需重启）；
//   - go2rtc 默认按需连接：有人看才拉流，没人看自动断开，预览基本不占 CPU。
type Service struct {
	cfg *config.Config

	mu      sync.Mutex
	cmd     *exec.Cmd
	done    chan struct{}
	started bool

	httpCli *http.Client
}

func New(cfg *config.Config) *Service {
	return &Service{
		cfg:     cfg,
		httpCli: &http.Client{Timeout: 5 * time.Second},
	}
}

// Enabled 是否启用实时预览。
func (s *Service) Enabled() bool { return s.cfg.Preview.Enabled }

// BasePath 反向代理前缀，例如 /go2rtc。
func (s *Service) BasePath() string { return s.cfg.Preview.BasePath }

// StreamName 摄像头对应的 go2rtc 流名。
func (s *Service) StreamName(id int64) string { return fmt.Sprintf("cam-%d", id) }

// apiURL 拼 go2rtc 内部 API 地址（含 basePath）。
func (s *Service) apiURL(path string) string {
	p := s.cfg.Preview
	base := strings.TrimSuffix(p.BasePath, "/")
	return "http://" + p.Addr + base + "/" + strings.TrimPrefix(path, "/")
}

// Start 启动 go2rtc 子进程并等待其 API 就绪。
// 二进制缺失时返回错误，调用方决定是否降级（预览不可用但不影响主服务）。
func (s *Service) Start(ctx context.Context) error {
	if !s.Enabled() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}

	bin := s.cfg.Preview.Binary
	if err := checkExecutable(bin); err != nil {
		return fmt.Errorf("go2rtc binary: %w", err)
	}

	confPath := filepath.Join(s.cfg.Storage.BaseDir, "go2rtc.yaml")
	if err := writeConfig(confPath, s.cfg.Preview); err != nil {
		return fmt.Errorf("write go2rtc config: %w", err)
	}

	logPath := filepath.Join(s.cfg.Storage.BaseDir, "logs", "go2rtc.log")
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open go2rtc log: %w", err)
	}

	cmd := exec.Command(bin, "-config", confPath)
	cmd.Stdout = lf
	cmd.Stderr = lf
	if err := cmd.Start(); err != nil {
		_ = lf.Close()
		return fmt.Errorf("start go2rtc: %w", err)
	}

	done := make(chan struct{})
	go func() {
		err := cmd.Wait()
		if err != nil {
			log.Printf("[preview] go2rtc exited: %v (log: %s)", err, logPath)
		}
		_ = lf.Close()
		// 先关 done 再拿锁：Start 里会持锁等 done，顺序反了会死锁
		close(done)
		s.mu.Lock()
		if s.cmd == cmd {
			s.started = false
		}
		s.mu.Unlock()
	}()

	s.cmd = cmd
	s.done = done
	s.started = true

	// 等待 API 就绪
	deadline := time.Now().Add(s.cfg.Preview.StartTimeout)
	probeURL := s.apiURL("api/streams")
	for time.Now().Before(deadline) {
		if s.health(probeURL) {
			log.Printf("[preview] go2rtc 就绪: %s (config: %s)", s.apiURL(""), confPath)
			return nil
		}
		select {
		case <-ctx.Done():
			s.stopLocked()
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}

	s.stopLocked()
	return fmt.Errorf("go2rtc 未在 %s 内就绪（请检查 %s）", s.cfg.Preview.StartTimeout, logPath)
}

func checkExecutable(bin string) error {
	if strings.ContainsRune(bin, os.PathSeparator) {
		fi, err := os.Stat(bin)
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return fmt.Errorf("%s is a directory", bin)
		}
		return nil
	}
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("%s not found in PATH", bin)
	}
	return nil
}

func writeConfig(path string, p config.PreviewConfig) error {
	cfg := map[string]any{
		"api": map[string]any{
			"listen":    p.Addr,
			"base_path": p.BasePath,
		},
		"log": map[string]any{
			"level": "info",
		},
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (s *Service) health(u string) bool {
	resp, err := s.httpCli.Get(u)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Stop 停止 go2rtc 子进程。
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

func (s *Service) stopLocked() {
	if !s.started || s.cmd == nil || s.cmd.Process == nil {
		s.started = false
		return
	}
	done := s.done
	_ = s.cmd.Process.Signal(os.Interrupt)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = s.cmd.Process.Kill()
		<-done
	}
	s.started = false
}

// Sync 让 go2rtc 的流与数据库里的摄像头一致：
// 启用的摄像头 PUT 进去，数据库里没有的流删掉。
func (s *Service) Sync(ctx context.Context, cams []camera.Camera) error {
	if !s.Enabled() {
		return nil
	}
	s.mu.Lock()
	started := s.started
	s.mu.Unlock()
	if !started {
		return fmt.Errorf("go2rtc 未运行")
	}

	known := make(map[string]bool, len(cams))
	for _, c := range cams {
		if !c.Enabled {
			continue
		}
		name := s.StreamName(c.ID)
		known[name] = true
		if err := s.upsert(ctx, name, c); err != nil {
			log.Printf("[preview] 同步流 %s 失败: %v", name, err)
		}
	}

	existing, err := s.listStreams(ctx)
	if err != nil {
		return err
	}
	for name := range existing {
		if !known[name] {
			if err := s.remove(ctx, name); err != nil {
				log.Printf("[preview] 删除流 %s 失败: %v", name, err)
			}
		}
	}
	return nil
}

// sourceURL 组装 go2rtc 源地址：rtsp://user:pass@host/path#transport=tcp#media=video
// 注意：go2rtc 的 fragment 多参数用 "#" 分隔（rtsp#p1#p2），不是 "&"；
// 用 "&" 会被 go2rtc 解析成单个参数（transport=tcp&media=video），导致拨号失败。
func (s *Service) sourceURL(c camera.Camera) string {
	u := ffmpeg.BuildRTSPURL(c.RtspURL, c.Username, c.Password)
	opts := make([]string, 0, 2)
	if t := s.cfg.Preview.RTSPTransport; t != "" {
		opts = append(opts, "transport="+t)
	}
	if m := s.cfg.Preview.Media; m != "" {
		opts = append(opts, "media="+m)
	}
	if len(opts) > 0 {
		u += "#" + strings.Join(opts, "#")
	}
	return u
}

func (s *Service) upsert(ctx context.Context, name string, c camera.Camera) error {
	q := url.Values{}
	q.Set("name", name)
	q.Set("src", s.sourceURL(c))
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.apiURL("api/streams?"+q.Encode()), nil)
	if err != nil {
		return err
	}
	resp, err := s.httpCli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("PUT api/streams: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (s *Service) listStreams(ctx context.Context) (map[string]json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiURL("api/streams"), nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.httpCli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET api/streams: %s", resp.Status)
	}
	var m map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Service) remove(ctx context.Context, name string) error {
	q := url.Values{}
	q.Set("src", name)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.apiURL("api/streams?"+q.Encode()), nil)
	if err != nil {
		return err
	}
	resp, err := s.httpCli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("DELETE api/streams: %s", resp.Status)
	}
	return nil
}
