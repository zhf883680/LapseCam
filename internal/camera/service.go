package camera

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"

	"timelapse/config"
	"timelapse/internal/database"
	"timelapse/internal/ffmpeg"
)

var (
	ErrNotFound = errors.New("camera not found")
)

// Service 负责摄像头的 CRUD、测试连接与在线状态维护。
type Service struct {
	db   *sql.DB
	cfg  *config.Config
	ff   *ffmpeg.Service
	mu   sync.Mutex // 保护探测状态更新
}

func New(db *sql.DB, cfg *config.Config, ff *ffmpeg.Service) *Service {
	return &Service{db: db, cfg: cfg, ff: ff}
}

// List 返回所有摄像头（按创建时间正序）。
func (s *Service) List() ([]Camera, error) {
	rows, err := s.db.Query(`SELECT id, name, rtsp_url, username, password, enabled, online, last_seen_at, created_at, updated_at FROM cameras ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Camera, 0)
	for rows.Next() {
		c, err := scanCamera(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Get 返回单个摄像头。
func (s *Service) Get(id int64) (Camera, error) {
	row := s.db.QueryRow(`SELECT id, name, rtsp_url, username, password, enabled, online, last_seen_at, created_at, updated_at FROM cameras WHERE id = ?`, id)
	c, err := scanCamera(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Camera{}, ErrNotFound
	}
	return c, err
}

// Create 新建摄像头。
func (s *Service) Create(in CameraInput) (Camera, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.RtspURL = strings.TrimSpace(in.RtspURL)
	if in.Name == "" {
		return Camera{}, errors.New("name is required")
	}
	if in.RtspURL == "" {
		return Camera{}, errors.New("rtspUrl is required")
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	now := database.NowUTC()
	res, err := s.db.Exec(`INSERT INTO cameras (name, rtsp_url, username, password, enabled, online, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?)`,
		in.Name, in.RtspURL, in.Username, in.Password, boolToInt(enabled), now, now)
	if err != nil {
		return Camera{}, err
	}
	id, _ := res.LastInsertId()
	return s.Get(id)
}

// Update 修改摄像头。password 为空表示不修改密码。
func (s *Service) Update(id int64, in CameraInput) (Camera, error) {
	old, err := s.Get(id)
	if err != nil {
		return Camera{}, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = old.Name
	}
	rtsp := strings.TrimSpace(in.RtspURL)
	if rtsp == "" {
		rtsp = old.RtspURL
	}
	username := in.Username
	if username == "" {
		username = old.Username
	}
	password := in.Password
	if password == "" {
		password = old.Password
	}
	enabled := old.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	now := database.NowUTC()
	if _, err := s.db.Exec(`UPDATE cameras SET name=?, rtsp_url=?, username=?, password=?, enabled=?, updated_at=? WHERE id=?`,
		name, rtsp, username, password, boolToInt(enabled), now, id); err != nil {
		return Camera{}, err
	}
	return s.Get(id)
}

// Delete 删除摄像头（调用方负责检查是否被任务引用）。
func (s *Service) Delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM cameras WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Test 用 ffprobe 测试摄像头连通性。
func (s *Service) Test(id int64) (TestResult, error) {
	c, err := s.Get(id)
	if err != nil {
		return TestResult{}, err
	}
	url := ffmpeg.BuildRTSPURL(c.RtspURL, c.Username, c.Password)
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.FFmpeg.ProbeTimeout+5*time.Second)
	defer cancel()

	info, err := s.ff.Probe(ctx, url, s.cfg.FFmpeg.ProbeTimeout)
	if err != nil {
		s.setOnline(c.ID, false, nil)
		return TestResult{OK: false, Message: sanitizeCreds(err.Error(), url, c.RtspURL)}, nil
	}
	now := time.Now()
	s.setOnline(c.ID, true, &now)
	return TestResult{
		OK:      true,
		Message: "ok",
		Stream: &Stream{
			Width:        info.Width,
			Height:       info.Height,
			CodecName:    info.CodecName,
			AvgFrameRate: info.AvgFrameRate,
		},
	}, nil
}

// MarkActivity 由任务 worker 上报摄像头最近一次连通/断开，用于在线状态联动。
func (s *Service) MarkActivity(id int64, online bool, when time.Time) {
	s.setOnline(id, online, &when)
}

// StartChecker 周期性探测所有启用摄像头的在线状态。
func (s *Service) StartChecker(ctx context.Context) {
	interval := time.Duration(s.cfg.Scheduler.CameraCheckSeconds) * time.Second
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkAll(ctx)
		}
	}
}

func (s *Service) checkAll(ctx context.Context) {
	cams, err := s.List()
	if err != nil {
		return
	}
	for _, c := range cams {
		if !c.Enabled {
			continue
		}
		url := ffmpeg.BuildRTSPURL(c.RtspURL, c.Username, c.Password)
		info, err := s.ff.Probe(ctx, url, s.cfg.FFmpeg.ProbeTimeout)
		if err == nil && info != nil {
			now := time.Now()
			s.setOnline(c.ID, true, &now)
		} else {
			s.setOnline(c.ID, false, nil)
		}
	}
}

func (s *Service) setOnline(id int64, online bool, when *time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if when == nil {
		s.db.Exec(`UPDATE cameras SET online=?, last_seen_at=last_seen_at WHERE id=?`, boolToInt(online), id)
		return
	}
	s.db.Exec(`UPDATE cameras SET online=?, last_seen_at=? WHERE id=?`, boolToInt(online), when.UTC().Format(time.RFC3339), id)
}

// rowScanner 兼容 *sql.Row 与 *sql.Rows。
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCamera(r rowScanner) (Camera, error) {
	var c Camera
	var enabled, online int
	var lastSeen sql.NullString
	var createdAt, updatedAt string
	if err := r.Scan(&c.ID, &c.Name, &c.RtspURL, &c.Username, &c.Password, &enabled, &online, &lastSeen, &createdAt, &updatedAt); err != nil {
		return Camera{}, err
	}
	c.Enabled = enabled == 1
	c.Online = online == 1
	c.HasPassword = c.Password != ""
	c.LastSeenAt = nil
	if lastSeen.Valid && lastSeen.String != "" {
		if t, err := time.Parse(time.RFC3339, lastSeen.String); err == nil {
			c.LastSeenAt = &t
		}
	}
	c.CreatedAt = database.ParseTime(createdAt)
	c.UpdatedAt = database.ParseTime(updatedAt)
	return c, nil
}

// sanitizeCreds 去掉报错信息中的 RTSP 明文密码。
func sanitizeCreds(msg, built, raw string) string {
	return strings.ReplaceAll(msg, built, raw)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

