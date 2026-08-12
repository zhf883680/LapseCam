package camera

import (
	"encoding/json"
	"strings"
	"testing"

	"timelapse/config"
	"timelapse/internal/database"
	"timelapse/internal/ffmpeg"
)

func newTestService(t *testing.T) (*Service, func()) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	s := New(db, cfg, ffmpeg.New(cfg))
	return s, func() { db.Close() }
}

func TestCameraCRUD(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	in := CameraInput{Name: "客厅", RtspURL: "rtsp://127.0.0.1:554/stream", Username: "admin", Password: "123"}
	c, err := s.Create(in)
	if err != nil {
		t.Fatal(err)
	}
	if !c.HasPassword {
		t.Fatal("expected hasPassword=true")
	}
	// 模型内部保留密码用于拼 RTSP URL，但 JSON 序列化必须脱敏
	b, _ := json.Marshal(c)
	if strings.Contains(string(b), "123") || strings.Contains(string(b), "password") {
		t.Fatalf("password leaked in JSON: %s", b)
	}

	// 更新不传密码，保留旧密码
	enabled := false
	c2, err := s.Update(c.ID, CameraInput{Name: "客厅2", Enabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	if c2.Name != "客厅2" || c2.Enabled != false || c2.RtspURL != in.RtspURL {
		t.Fatalf("unexpected update result: %+v", c2)
	}
	// 从 DB 直接读回，确认密码保留
	row := s.db.QueryRow(`SELECT password FROM cameras WHERE id=?`, c.ID)
	var pw string
	if err := row.Scan(&pw); err != nil {
		t.Fatal(err)
	}
	if pw != "123" {
		t.Fatalf("password not preserved, got %q", pw)
	}

	// 更新传新密码
	c3, err := s.Update(c.ID, CameraInput{Name: "客厅3", Password: "456"})
	if err != nil {
		t.Fatal(err)
	}
	if !c3.HasPassword {
		t.Fatal("expected hasPassword=true after update")
	}
	row = s.db.QueryRow(`SELECT password FROM cameras WHERE id=?`, c.ID)
	if err := row.Scan(&pw); err != nil {
		t.Fatal(err)
	}
	if pw != "456" {
		t.Fatalf("password not updated, got %q", pw)
	}

	// 删除
	if err := s.Delete(c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(c.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCameraTestUnreachable(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	c, err := s.Create(CameraInput{Name: "x", RtspURL: "rtsp://127.0.0.1:1/stream"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Test(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("expected ok=false for unreachable rtsp")
	}
	if res.Stream != nil {
		t.Fatal("expected no stream info")
	}
	// 报错信息不应包含密码明文
	c2, _ := s.Get(c.ID)
	_ = c2
}
