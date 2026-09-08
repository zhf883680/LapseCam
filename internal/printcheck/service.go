package printcheck

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"timelapse/config"
	"timelapse/internal/database"
	"timelapse/internal/storage"
	"timelapse/internal/vision"
)

// Service 负责「逐层截图后自动分析最近 N 张帧」：
// 取帧 → OpenAI 兼容视觉模型（默认 DeepSeek）整体判断 → 落库 → 连续异常告警 Webhook。
type Service struct {
	db  *sql.DB
	cfg *config.Config
	vs  *vision.Service
	st  *storage.Service

	mu      sync.Mutex
	busy    map[int64]bool // 每个任务同时只跑一个分析
	pending map[int64]bool // 分析期间又来新截图 → 结束后补一次
}

func New(db *sql.DB, cfg *config.Config, vs *vision.Service, st *storage.Service) *Service {
	return &Service{
		db:      db,
		cfg:     cfg,
		vs:      vs,
		st:      st,
		busy:    make(map[int64]bool),
		pending: make(map[int64]bool),
	}
}

// Enabled 该功能是否可用（vision 开启、配了 Key、analyzeFrames>0）。
func (s *Service) Enabled() bool {
	return s.cfg.Vision.Enabled && s.cfg.Vision.AnalyzeFrames > 0 && s.vs != nil && s.vs.Enabled()
}

// CheckRecent 分析某打印任务最近 N 帧并落库/告警。
// 由 /api/quick/snapshot 截完一层图后在后台调用；同一任务并发调用只串行执行，
// 若执行期间又有新截图则结束后自动补跑一次。
func (s *Service) CheckRecent(ctx context.Context, taskID int64) error {
	if !s.Enabled() {
		return nil
	}
	s.mu.Lock()
	if s.busy[taskID] {
		s.pending[taskID] = true
		s.mu.Unlock()
		return nil
	}
	s.busy[taskID] = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.busy[taskID] = false
		rerun := s.pending[taskID]
		s.pending[taskID] = false
		s.mu.Unlock()
		if rerun {
			go s.CheckRecent(context.Background(), taskID)
		}
	}()

	n := s.cfg.Vision.AnalyzeFrames
	paths := lastNFrameFiles(s.st.FramesDir(taskID), n)
	if len(paths) == 0 {
		return errors.New("no frames to analyze")
	}
	images := make([][]byte, 0, len(paths))
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err == nil && len(b) > 0 {
			images = append(images, b)
		}
	}
	if len(images) == 0 {
		return errors.New("no readable frames")
	}

	res, err := s.vs.Analyze(ctx, images)
	now := time.Now()
	if err != nil {
		s.insertCheck(taskID, vision.StatusUnknown, 0, "AI 分析失败: "+err.Error(), "", false, now)
		return err
	}
	if res == nil {
		return errors.New("empty vision result")
	}

	abnormal := isAbnormal(res.Status, res.Confidence, s.cfg.Vision.MinConfidence)

	// 只要判异常就先留存一份现场图（最新一帧）到 data/vision/task-{id}/events/。
	// 出片/清理会删掉 frames 里的中间帧，但这里的留存副本不受影响，审计随时能看。
	imgPath := ""
	if abnormal {
		p, imgErr := s.saveCheckImage(taskID, images[len(images)-1], res.Status, now)
		if imgErr != nil {
			log.Printf("[printcheck] save alert image failed: %v", imgErr)
		} else {
			imgPath = p
		}
	}
	id := s.insertCheck(taskID, res.Status, res.Confidence, res.Reason, imgPath, false, now)
	if !abnormal || id == 0 {
		return nil
	}

	// 连续异常次数 & 冷却判定（达标才算“告警”，才发 Bark/Webhook）
	streak := s.trailingStreak(taskID)
	lastAlert := s.lastAlertTime(taskID)
	cooldown := time.Duration(s.cfg.Vision.CooldownSeconds) * time.Second
	if !decideAlert(s.cfg.Vision.FailureStreak, streak, lastAlert, cooldown, now) {
		return nil
	}

	_, _ = s.db.Exec(`UPDATE vision_checks SET alert=1 WHERE id=?`, id)
	c := Check{
		ID: id, TaskID: taskID, Status: res.Status, Confidence: res.Confidence,
		Reason: res.Reason, ImagePath: imgPath, Alert: true, CreatedAt: now,
	}
	s.sendWebhook(taskID, c)
	s.sendBark(c)
	return nil
}

// Latest 返回某任务最近一次分析结果。
func (s *Service) Latest(taskID int64) (Check, bool, error) {
	row := s.db.QueryRow(`SELECT id, task_id, status, confidence, reason, image_path, alert, created_at
		FROM vision_checks WHERE task_id=? ORDER BY id DESC LIMIT 1`, taskID)
	c, err := scanCheck(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Check{}, false, nil
	}
	if err != nil {
		return Check{}, false, err
	}
	return c, true, nil
}

// List 返回某任务最近 limit 条分析记录。
func (s *Service) List(taskID int64, limit int) ([]Check, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT id, task_id, status, confidence, reason, image_path, alert, created_at
		FROM vision_checks WHERE task_id=? ORDER BY id DESC LIMIT ?`, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Check, 0)
	for rows.Next() {
		c, err := scanCheck(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListAll 返回最近 limit 条 AI 分析记录（跨任务，审计用），附任务/摄像头名。
func (s *Service) ListAll(limit int) ([]Check, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT c.id, c.task_id, t.name, cam.name, c.status, c.confidence,
		c.reason, c.image_path, c.alert, c.created_at
		FROM vision_checks c
		LEFT JOIN timelapse_tasks t ON t.id = c.task_id
		LEFT JOIN cameras cam ON cam.id = t.camera_id
		ORDER BY c.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Check, 0)
	for rows.Next() {
		var c Check
		var alert int
		var createdAt string
		var taskName, camName sql.NullString
		if err := rows.Scan(&c.ID, &c.TaskID, &taskName, &camName, &c.Status, &c.Confidence,
			&c.Reason, &c.ImagePath, &alert, &createdAt); err != nil {
			return nil, err
		}
		c.TaskName = taskName.String
		c.CameraName = camName.String
		c.Alert = alert == 1
		c.CreatedAt = database.ParseTime(createdAt)
		if c.ImagePath != "" {
			c.ImageURL = fmt.Sprintf("/api/checks/%d/image", c.ID)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CheckImagePath 返回某条分析记录的现场图存储路径。
func (s *Service) CheckImagePath(id int64) (string, error) {
	var p string
	err := s.db.QueryRow(`SELECT image_path FROM vision_checks WHERE id=?`, id).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("check not found")
	}
	if err != nil {
		return "", err
	}
	if p == "" {
		return "", errors.New("check has no image")
	}
	return p, nil
}

// ---- 内部 ----

type rowScanner interface {
	Scan(...any) error
}

func scanCheck(r rowScanner) (Check, error) {
	var c Check
	var alert int
	var createdAt string
	if err := r.Scan(&c.ID, &c.TaskID, &c.Status, &c.Confidence, &c.Reason, &c.ImagePath, &alert, &createdAt); err != nil {
		return Check{}, err
	}
	c.Alert = alert == 1
	c.CreatedAt = database.ParseTime(createdAt)
	c.ImageURL = fmt.Sprintf("/api/quick/checks/%d/image", c.ID)
	return c, nil
}

func (s *Service) insertCheck(taskID int64, status string, confidence float64, reason, imgPath string, alert bool, when time.Time) int64 {
	res, err := s.db.Exec(`INSERT INTO vision_checks (task_id, status, confidence, reason, image_path, alert, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		taskID, status, confidence, reason, imgPath, boolToInt(alert), when.UTC().Format(time.RFC3339))
	if err != nil {
		log.Printf("[printcheck] insert check failed: %v", err)
		return 0
	}
	id, _ := res.LastInsertId()
	s.pruneOld(taskID)
	return id
}

// pruneOld 控制历史规模：每个任务保留最近 500 条，全局保留最近 5000 条，
// 避免长时间/多次打印后 vision_checks 无限增长。
func (s *Service) pruneOld(taskID int64) {
	_, _ = s.db.Exec(`DELETE FROM vision_checks WHERE task_id=? AND id NOT IN (
		SELECT id FROM vision_checks WHERE task_id=? ORDER BY id DESC LIMIT 500)`, taskID, taskID)
	_, _ = s.db.Exec(`DELETE FROM vision_checks WHERE id NOT IN (
		SELECT id FROM vision_checks ORDER BY id DESC LIMIT 5000)`)
}

// trailingStreak 统计该任务最近连续异常次数（含刚插入的一条）。
func (s *Service) trailingStreak(taskID int64) int {
	rows, err := s.db.Query(`SELECT status, confidence FROM vision_checks WHERE task_id=? ORDER BY id DESC LIMIT 200`, taskID)
	if err != nil {
		return 0
	}
	defer rows.Close()
	streak := 0
	for rows.Next() {
		var status string
		var conf float64
		if err := rows.Scan(&status, &conf); err != nil {
			break
		}
		if !isAbnormal(status, conf, s.cfg.Vision.MinConfidence) {
			break
		}
		streak++
	}
	return streak
}

func (s *Service) lastAlertTime(taskID int64) time.Time {
	var created sql.NullString
	err := s.db.QueryRow(`SELECT MAX(created_at) FROM vision_checks WHERE task_id=? AND alert=1`, taskID).Scan(&created)
	if err != nil || !created.Valid || created.String == "" {
		return time.Time{}
	}
	return database.ParseTime(created.String)
}

func (s *Service) saveCheckImage(taskID int64, data []byte, status string, when time.Time) (string, error) {
	dir := s.st.VisionEventsDir(taskID)
	if err := s.st.EnsureDir(dir); err != nil {
		return "", err
	}
	if status == "" {
		status = vision.StatusUnknown
	}
	name := fmt.Sprintf("%s-%s.jpg", when.Format("20060102-150405.000"), status)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// sendWebhook 推送告警（失败只记日志，不影响截图流程）。
func (s *Service) sendWebhook(taskID int64, c Check) {
	wh := s.cfg.Vision.Webhook
	if !wh.Enabled || wh.URL == "" {
		return
	}
	payload := map[string]any{
		"taskId":     taskID,
		"status":     c.Status,
		"confidence": c.Confidence,
		"reason":     c.Reason,
		"timestamp":  c.CreatedAt.Local().Format(time.RFC3339),
	}
	if c.ID > 0 {
		payload["image"] = fmt.Sprintf("/api/quick/checks/%d/image", c.ID)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[printcheck] webhook marshal failed: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, bytes.NewReader(body))
	if err != nil {
		log.Printf("[printcheck] webhook request failed: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[printcheck] webhook send failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("[printcheck] webhook http %d", resp.StatusCode)
	}
}

// lastNFrameFiles 返回帧目录里最新 N 个 %06d.jpg 的完整路径（按帧号升序）。
func lastNFrameFiles(dir string, n int) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	nums := make([]int, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".jpg") {
			continue
		}
		if num, err := strconv.Atoi(strings.TrimSuffix(name, ".jpg")); err == nil {
			nums = append(nums, num)
		}
	}
	sort.Ints(nums)
	if len(nums) > n {
		nums = nums[len(nums)-n:]
	}
	out := make([]string, 0, len(nums))
	for _, num := range nums {
		out = append(out, filepath.Join(dir, fmt.Sprintf("%06d.jpg", num)))
	}
	return out
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
