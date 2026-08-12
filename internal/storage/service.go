package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"timelapse/config"
)

// Service 负责数据目录（frames / videos）的路径与清理。
type Service struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Service {
	return &Service{cfg: cfg}
}

// FramesDir 返回某任务的抽帧目录，例如 data/frames/task-1。
func (s *Service) FramesDir(taskID int64) string {
	return filepath.Join(s.cfg.Storage.BaseDir, s.cfg.Storage.FramesDir, fmt.Sprintf("task-%d", taskID))
}

// VideosDir 返回某任务的视频目录，例如 data/videos/task-1。
func (s *Service) VideosDir(taskID int64) string {
	return filepath.Join(s.cfg.Storage.BaseDir, s.cfg.Storage.VideosDir, fmt.Sprintf("task-%d", taskID))
}

// EnsureDir 递归创建目录。
func (s *Service) EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

// CountFiles 统计目录下文件数量（含子目录，深度 1）。
func (s *Service) CountFiles(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.Type().IsRegular() {
			n++
		}
	}
	return n, nil
}

// RemoveDir 递归删除目录（不存在时不报错）。
func (s *Service) RemoveDir(dir string) error {
	return os.RemoveAll(dir)
}

// RemoveFile 删除文件（不存在时不报错）。
func (s *Service) RemoveFile(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

var unsafeChars = regexp.MustCompile(`[\\/:*?"<>|\s]+`)

// SanitizeFilename 清理文件名中的非法字符，用于生成视频文件名。
func (s *Service) SanitizeFilename(name string) string {
	name = unsafeChars.ReplaceAllString(strings.TrimSpace(name), "_")
	name = strings.Trim(name, "_.")
	if name == "" {
		name = "timelapse"
	}
	return name
}
