package timelapse

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"timelapse/internal/database"
)

// 测试辅助：直接插入任务行（避免依赖真实摄像头与 worker）。
func insertTaskRow(s *Service, id int64, status, name string) {
	now := database.NowUTC()
	_, err := s.db.Exec(`INSERT INTO timelapse_tasks
		(id, name, camera_id, interval_seconds, output_fps, width, height, start_at, end_at, status, error_message, created_at, updated_at)
		VALUES (?, ?, 1, 10, 30, 1280, 720, ?, NULL, ?, '', ?, ?)`,
		id, name, now, status, now, now)
	if err != nil {
		panic(err)
	}
}

// 测试辅助：插入视频记录行。
func insertRecordRow(s *Service, id, taskID, frameCount int64, createdAt, filePath string) {
	_, err := s.db.Exec(`INSERT INTO timelapse_records
		(id, task_id, start_time, end_time, frame_count, duration_seconds, file_path, file_size, status, error_message, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 30, ?, 1000, ?, '', ?, ?)`,
		id, taskID, createdAt, createdAt, frameCount, filePath, RecordSuccess, createdAt, createdAt)
	if err != nil {
		panic(err)
	}
}

// 测试辅助：创建某任务的抽帧目录并写入 n 个假帧。
func mkFrames(s *Service, id int64, n int) {
	dir := s.storage.FramesDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}
	for i := 1; i <= n; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%06d.jpg", i)), []byte("jpeg"), 0o644); err != nil {
			panic(err)
		}
	}
}

func TestCleanupFramesCompletedOnly(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	insertTaskRow(s, 1, StatusCompleted, "done")
	insertTaskRow(s, 2, StatusFailed, "fail")
	mkFrames(s, 1, 5)
	mkFrames(s, 2, 3)
	if err := os.WriteFile(s.storage.MarkersFile(1), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	stats, err := s.Cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.storage.FramesDir(1)); !os.IsNotExist(err) {
		t.Fatal("completed 任务的中间帧应被删除")
	}
	if _, err := os.Stat(s.storage.FramesDir(2)); err != nil {
		t.Fatal("failed 任务的中间帧应保留")
	}
	if _, err := os.Stat(s.storage.MarkersFile(1)); !os.IsNotExist(err) {
		t.Fatal("completed 任务的 layers 标记应被删除")
	}
	if stats.CleanedFrames != 1 {
		t.Fatalf("CleanedFrames = %d, want 1", stats.CleanedFrames)
	}
	if stats.FreedBytes <= 0 {
		t.Fatalf("FreedBytes = %d, want > 0", stats.FreedBytes)
	}
}

func TestCleanupRetention(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()
	s.cfg.Cleanup.VideoRetentionDays = 5

	old := time.Now().Add(-10 * 24 * time.Hour).UTC().Format(time.RFC3339)
	recent := time.Now().UTC().Format(time.RFC3339)

	insertTaskRow(s, 3, StatusCompleted, "t3") // 任务存在，videos/task-3 不视为孤儿

	// 同一任务两条记录：一条过期、一条新近，放在同一个 videos/task-3 目录
	if err := os.MkdirAll(s.storage.VideosDir(3), 0o755); err != nil {
		t.Fatal(err)
	}
	oldFile := filepath.Join(s.storage.VideosDir(3), "old.mp4")
	if err := os.WriteFile(oldFile, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	newFile := filepath.Join(s.storage.VideosDir(3), "new.mp4")
	if err := os.WriteFile(newFile, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	insertRecordRow(s, 1, 3, 100, old, oldFile)
	insertRecordRow(s, 2, 3, 50, recent, newFile)

	if _, err := s.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetRecord(1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("过期记录应被删除, got %v", err)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatal("过期视频文件应被删除")
	}
	if _, err := s.GetRecord(2); err != nil {
		t.Fatalf("新近记录应保留: %v", err)
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Fatal("新近视频文件应保留")
	}
}

func TestCleanupRetentionDisabled(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()
	s.cfg.Cleanup.VideoRetentionDays = 0 // 默认：保留全部

	old := time.Now().Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	insertTaskRow(s, 4, StatusCompleted, "t4") // 任务存在，videos/task-4 不视为孤儿
	if err := os.MkdirAll(s.storage.VideosDir(4), 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(s.storage.VideosDir(4), "old.mp4")
	if err := os.WriteFile(f, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	insertRecordRow(s, 9, 4, 10, old, f)

	if _, err := s.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetRecord(9); err != nil {
		t.Fatalf("retention 关闭时记录不应被删: %v", err)
	}
	if _, err := os.Stat(f); err != nil {
		t.Fatal("retention 关闭时文件不应被删")
	}
}

func TestCleanupOrphans(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	insertTaskRow(s, 1, StatusPending, "keep") // 非 completed，保证中间帧不被 cleanupFrames 删
	mkFrames(s, 1, 2)
	if err := os.MkdirAll(s.storage.VideosDir(1), 0o755); err != nil {
		t.Fatal(err)
	}

	// 无主残留
	if err := os.MkdirAll(s.storage.FramesDir(99), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.storage.VideosDir(99), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.storage.MarkersFile(99), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	logDir := filepath.Join(s.cfg.Storage.BaseDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "task-99.log"), []byte("log"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 格式不符的条目应被跳过
	if err := os.WriteFile(filepath.Join(s.cfg.Storage.BaseDir, s.cfg.Storage.FramesDir, "tmp.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	stats, err := s.Cleanup()
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{s.storage.FramesDir(99), s.storage.VideosDir(99)} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("孤儿目录应被删除: %s", dir)
		}
	}
	for _, f := range []string{s.storage.MarkersFile(99), filepath.Join(logDir, "task-99.log")} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Fatalf("孤儿文件应被删除: %s", f)
		}
	}
	if _, err := os.Stat(s.storage.FramesDir(1)); err != nil {
		t.Fatal("有效任务的帧目录应保留")
	}
	if _, err := os.Stat(s.storage.VideosDir(1)); err != nil {
		t.Fatal("有效任务的视频目录应保留")
	}
	if stats.OrphanDirs != 2 {
		t.Fatalf("OrphanDirs = %d, want 2", stats.OrphanDirs)
	}
	if stats.OrphanFiles != 2 {
		t.Fatalf("OrphanFiles = %d, want 2", stats.OrphanFiles)
	}
}

func TestCleanupInProgress(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	s.cleanupMu.Lock()
	_, err := s.Cleanup()
	s.cleanupMu.Unlock()
	if !errors.Is(err, ErrCleanupInProgress) {
		t.Fatalf("err = %v, want ErrCleanupInProgress", err)
	}
}

func TestCleanupIdempotentStats(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	insertTaskRow(s, 1, StatusCompleted, "done")
	mkFrames(s, 1, 3)

	first, err := s.Cleanup()
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if first.CleanedFrames != 1 {
		t.Fatalf("first.CleanedFrames = %d, want 1", first.CleanedFrames)
	}
	if second.CleanedFrames != 0 || second.FreedBytes != 0 {
		t.Fatalf("重复清理统计应归零, got %+v", second)
	}
}

func TestEnrichFrameCountFallback(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	insertTaskRow(s, 1, StatusCompleted, "done")
	insertRecordRow(s, 1, 1, 123, database.NowUTC(), "/nonexistent.mp4")

	t1, err := s.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if t1.FrameCount != 123 {
		t.Fatalf("FrameCount = %d, want 123（回退到最近 success 记录）", t1.FrameCount)
	}

	insertTaskRow(s, 2, StatusCompleted, "nodata")
	t2, err := s.Get(2)
	if err != nil {
		t.Fatal(err)
	}
	if t2.FrameCount != 0 {
		t.Fatalf("无 success 记录时 FrameCount = %d, want 0", t2.FrameCount)
	}
}
