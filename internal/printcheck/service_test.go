package printcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"timelapse/config"
	"timelapse/internal/database"
	"timelapse/internal/storage"
	"timelapse/internal/vision"
)

func newTestService(t *testing.T) (*Service, func()) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Vision.MinConfidence = 0.8
	st := storage.New(cfg)
	vs := vision.New(cfg)
	s := New(db, cfg, vs, st)
	return s, func() { db.Close() }
}

func TestDecideAlert(t *testing.T) {
	now := time.Now()
	cooldown := 5 * time.Minute
	last := now.Add(-time.Minute) // 1 分钟前告警过

	// 只在“刚好达到阈值”的上升沿发
	if decideAlert(2, 1, time.Time{}, cooldown, now) {
		t.Error("streak=1 < failureStreak=2 should not alert")
	}
	if !decideAlert(2, 2, time.Time{}, cooldown, now) {
		t.Error("first time reaching threshold should alert")
	}
	if decideAlert(2, 3, time.Time{}, cooldown, now) {
		t.Error("streak=3 beyond threshold (already alerting) should not re-alert")
	}
	// 冷却期内不重复发
	if decideAlert(2, 2, last, cooldown, now) {
		t.Error("re-alert within cooldown should be suppressed")
	}
	// 超过冷却期的新一轮可以再发
	if !decideAlert(2, 2, now.Add(-10*time.Minute), cooldown, now) {
		t.Error("new episode after cooldown should alert")
	}
}

func TestIsAbnormal(t *testing.T) {
	if !isAbnormal(vision.StatusSpaghetti, 0.91, 0.8) {
		t.Error("spaghetti high conf should be abnormal")
	}
	if isAbnormal(vision.StatusSpaghetti, 0.5, 0.8) {
		t.Error("low confidence should not count")
	}
	if isAbnormal(vision.StatusNormal, 0.99, 0.8) {
		t.Error("normal should not be abnormal")
	}
	if isAbnormal(vision.StatusUnknown, 1, 0.8) {
		t.Error("unknown should not be abnormal")
	}
}

func TestLastNFrameFiles(t *testing.T) {
	dir := t.TempDir()
	for n := 1; n <= 20; n++ {
		name := filepath.Join(dir, fmt.Sprintf("%06d.jpg", n))
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 干扰文件不计入
	_ = os.WriteFile(filepath.Join(dir, "layer-1.jpg"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "test-1.jpg"), []byte("x"), 0o644)

	got := lastNFrameFiles(dir, 5)
	if len(got) != 5 {
		t.Fatalf("len = %d, want 5", len(got))
	}
	// 取最新 5 个帧号，且升序
	want := []string{"000016.jpg", "000017.jpg", "000018.jpg", "000019.jpg", "000020.jpg"}
	for i, w := range want {
		if filepath.Base(got[i]) != w {
			t.Errorf("got[%d] = %s, want %s", i, filepath.Base(got[i]), w)
		}
	}

	// 超过目录文件数时返回全部
	if all := lastNFrameFiles(dir, 100); len(all) != 20 {
		t.Errorf("all len = %d, want 20", len(all))
	}
}

func TestTrailingStreakAndAlertTime(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	// 依次插入：abnormal, abnormal, normal, abnormal → 尾部 streak=1；lastAlert 取 alert=1 的最新时间
	now := time.Now()
	s.insertCheck(1, vision.StatusSpaghetti, 0.91, "a", "", false, now.Add(-4*time.Minute))
	s.insertCheck(1, vision.StatusClog, 0.9, "b", "", true, now.Add(-3*time.Minute))
	s.insertCheck(1, vision.StatusNormal, 0.1, "c", "", false, now.Add(-2*time.Minute))
	s.insertCheck(1, vision.StatusMaterialBuildup, 0.95, "d", "", false, now.Add(-time.Minute))

	if streak := s.trailingStreak(1); streak != 1 {
		t.Errorf("streak = %d, want 1", streak)
	}
	la := s.lastAlertTime(1)
	if la.IsZero() {
		t.Fatal("lastAlertTime should not be zero")
	}
	if la.Sub(now.Add(-3*time.Minute)) > time.Second {
		t.Errorf("lastAlertTime = %v, want ~now-3min", la)
	}

	// 再插一条 abnormal → streak=2
	s.insertCheck(1, vision.StatusSpaghetti, 0.92, "e", "", false, now)
	if streak := s.trailingStreak(1); streak != 2 {
		t.Errorf("streak = %d, want 2", streak)
	}
	// 已过冷却（la≈now-3min，再等 4min 即超过 5min 冷却）→ 可以再告警
	if !decideAlert(2, 2, la, time.Duration(s.cfg.Vision.CooldownSeconds)*time.Second, now.Add(4*time.Minute)) {
		t.Error("new episode after cooldown should alert")
	}
	// 冷却期内 → 不告警
	if decideAlert(2, 2, la, time.Duration(s.cfg.Vision.CooldownSeconds)*time.Second, now.Add(-time.Minute)) {
		t.Error("within cooldown should not alert")
	}
}

func TestLatestAndList(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	if _, found, err := s.Latest(1); err != nil || found {
		t.Errorf("Latest on empty = found %v, err %v", found, err)
	}
	now := time.Now()
	s.insertCheck(1, vision.StatusNormal, 0.05, "ok", "", false, now.Add(-time.Minute))
	s.insertCheck(1, vision.StatusSpaghetti, 0.95, "bad", "", true, now)

	latest, found, err := s.Latest(1)
	if err != nil || !found {
		t.Fatalf("Latest = found %v, err %v", found, err)
	}
	if latest.Status != vision.StatusSpaghetti || !latest.Alert {
		t.Errorf("latest = %+v", latest)
	}
	if latest.ImageURL != "/api/quick/checks/2/image" {
		t.Errorf("imageUrl = %q", latest.ImageURL)
	}

	checks, err := s.List(1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 2 {
		t.Errorf("list len = %d, want 2", len(checks))
	}
	// 按时间倒序：最新在前
	if checks[0].ID < checks[1].ID {
		t.Error("list should be newest first")
	}
}

func TestUploadAlertImage(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	dir := t.TempDir()
	img := filepath.Join(dir, "x.jpg")
	if err := os.WriteFile(img, []byte("jpegdata"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 构建请求：URL 路径、鉴权头、query、file 字段
	s.cfg.Vision.ImageHost = config.ImageHostConfig{
		Enabled: true, BaseURL: "https://img.example.com", APIKey: "test-token",
		AuthCode: "code1", UploadChannel: "cfr2", UploadFolder: "lapsecam",
		ReturnFormat: "full",
	}
	req, err := s.buildImgHostRequest(s.cfg.Vision.ImageHost, img)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Path != "/upload" {
		t.Errorf("path = %s", req.URL.Path)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer test-token" {
		t.Errorf("auth header = %q", got)
	}
	q := req.URL.Query()
	if q.Get("uploadChannel") != "cfr2" || q.Get("uploadFolder") != "lapsecam" ||
		q.Get("returnFormat") != "full" || q.Get("authCode") != "code1" {
		t.Errorf("query = %v", q)
	}
	if ct := req.Header.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/form-data") {
		t.Errorf("content-type = %q", ct)
	}

	// 解析响应：优先 publicUrl；否则相对 src 用 base 拼接
	u, err := parseImgHostURL([]byte(`[{"src":"/file/abc.jpg","publicUrl":"https://img.example.com/abc.jpg"}]`), "https://img.example.com")
	if err != nil || u != "https://img.example.com/abc.jpg" {
		t.Errorf("parse publicUrl = %q, err %v", u, err)
	}
	u, err = parseImgHostURL([]byte(`[{"src":"/file/rel.jpg"}]`), "https://img.example.com")
	if err != nil || u != "https://img.example.com/file/rel.jpg" {
		t.Errorf("parse relative src = %q, err %v", u, err)
	}
	if _, err = parseImgHostURL([]byte(`[]`), "https://img.example.com"); err == nil {
		t.Error("empty array should error")
	}

	// baseUrl 为空 → 报错；未启用 → 不请求返回空
	if _, err = s.buildImgHostRequest(config.ImageHostConfig{BaseURL: "", APIKey: "t"}, img); err == nil {
		t.Error("empty baseUrl should error")
	}
	if _, err = s.buildImgHostRequest(config.ImageHostConfig{BaseURL: "https://img.example.com"}, img); err == nil {
		t.Error("no apiKey/authCode should error")
	}
	s.cfg.Vision.ImageHost.Enabled = false
	if u, _ := s.uploadAlertImage(img); u != "" {
		t.Errorf("disabled should return empty, got %q", u)
	}
}
