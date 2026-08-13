package timelapse

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

// ErrCleanupInProgress 清理正在进行中（手动触发时返回 409）。
var ErrCleanupInProgress = errors.New("cleanup already in progress")

// CleanupStats 一次清理的统计结果。
type CleanupStats struct {
	CleanedFrames int   `json:"cleanedFrames"` // 删除中间帧的任务数
	DeletedVideos int   `json:"deletedVideos"` // 按保留策略删除的视频记录数
	OrphanDirs    int   `json:"orphanDirs"`    // 删除的孤儿目录数
	OrphanFiles   int   `json:"orphanFiles"`   // 删除的孤儿文件数
	FreedBytes    int64 `json:"freedBytes"`    // 释放的磁盘空间（字节）
}

var (
	taskDirRe  = regexp.MustCompile(`^task-(\d+)$`)
	taskFileRe = regexp.MustCompile(`^task-(\d+)\.(?:layers\.json|log)$`)
)

// Cleanup 执行一轮数据清理（手动/自动共用），返回统计。
// 并发控制：进行中再次调用返回 ErrCleanupInProgress。
func (s *Service) Cleanup() (CleanupStats, error) {
	if !s.cleanupMu.TryLock() {
		return CleanupStats{}, ErrCleanupInProgress
	}
	defer s.cleanupMu.Unlock()

	var stats CleanupStats
	if s.cfg.Cleanup.RemoveFramesAfterEncode {
		s.cleanupFrames(&stats)
	}
	if s.cfg.Cleanup.VideoRetentionDays > 0 {
		s.cleanupOldVideos(&stats)
	}
	if s.cfg.Cleanup.RemoveOrphans {
		s.cleanupOrphans(&stats)
	}
	// 手动/自动成功执行后，重置自动清理计时（避免紧接着再跑一轮）
	s.lastCleanup = time.Now()
	return stats, nil
}

// maybeRunCleanup 由 scheduler 循环调用：到间隔且未在清理时，异步触发一次自动清理。
// 允许同一 tick 多次调用触发多个 goroutine；进行中的那轮会被 TryLock 拦截并丢弃，
// 符合"跳过进行中清理"的语义。
func (s *Service) maybeRunCleanup() {
	c := s.cfg.Cleanup
	if !c.Enabled {
		return
	}
	interval := time.Duration(c.IntervalHours) * time.Hour
	s.cleanupMu.Lock()
	due := s.lastCleanup.IsZero() || time.Since(s.lastCleanup) >= interval
	s.cleanupMu.Unlock()
	if !due {
		return
	}
	go func() {
		stats, err := s.Cleanup()
		if err != nil {
			if !errors.Is(err, ErrCleanupInProgress) {
				log.Printf("[cleanup] 自动清理失败: %v", err)
			}
			return
		}
		log.Printf("[cleanup] 自动清理完成: 中间帧=%d 旧视频=%d 孤儿目录=%d 孤儿文件=%d 释放=%d bytes",
			stats.CleanedFrames, stats.DeletedVideos, stats.OrphanDirs, stats.OrphanFiles, stats.FreedBytes)
	}()
}

// cleanupFrames 删除已完成（出片成功）任务的中间帧与层标记。
func (s *Service) cleanupFrames(stats *CleanupStats) {
	rows, err := s.db.Query(`SELECT id FROM timelapse_tasks WHERE status=?`, StatusCompleted)
	if err != nil {
		log.Printf("[cleanup] 查询 completed 任务失败: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			continue
		}
		framesDir := s.storage.FramesDir(id)
		if _, err := os.Stat(framesDir); err == nil {
			sz, _ := dirSize(framesDir) // 统计失败不阻塞删除，释放空间按 0 计
			if err := s.storage.RemoveDir(framesDir); err == nil {
				stats.CleanedFrames++
				stats.FreedBytes += sz
			} else {
				log.Printf("[cleanup] 删除中间帧 %s 失败: %v", framesDir, err)
			}
		}
		markers := s.storage.MarkersFile(id)
		if fi, err := os.Stat(markers); err == nil {
			if err := s.storage.RemoveFile(markers); err == nil {
				stats.FreedBytes += fi.Size()
			}
		}
	}
}

// cleanupOldVideos 按 videoRetentionDays 删除过期视频记录及文件。
func (s *Service) cleanupOldVideos(stats *CleanupStats) {
	cutoff := time.Now().AddDate(0, 0, -s.cfg.Cleanup.VideoRetentionDays).UTC().Format(time.RFC3339)
	rows, err := s.db.Query(`SELECT id, file_path FROM timelapse_records WHERE created_at < ?`, cutoff)
	if err != nil {
		log.Printf("[cleanup] 查询过期视频失败: %v", err)
		return
	}
	// SetMaxOpenConns(1)：先收齐结果并关闭 rows，再执行删除，避免单连接自死锁
	type recT struct {
		id   int64
		path string
	}
	var recs []recT
	for rows.Next() {
		var r recT
		if err := rows.Scan(&r.id, &r.path); err != nil {
			continue
		}
		recs = append(recs, r)
	}
	rows.Close()

	for _, r := range recs {
		if r.path != "" {
			var sz int64
			if fi, err := os.Stat(r.path); err == nil {
				sz = fi.Size()
			}
			if err := s.storage.RemoveFile(r.path); err == nil {
				stats.FreedBytes += sz
			}
			// 目录清空后顺带移除（非空目录报错忽略）
			_ = os.Remove(filepath.Dir(r.path))
		}
		if _, err := s.db.Exec(`DELETE FROM timelapse_records WHERE id=?`, r.id); err != nil {
			log.Printf("[cleanup] 删除视频记录 %d 失败: %v", r.id, err)
			continue
		}
		stats.DeletedVideos++
	}
}

// cleanupOrphans 删除数据库里已不存在任务的残留目录/文件。
// 并发安全：不用一次性 ID 快照，对每个候选在删除前逐条重查任务行，
// 存在则跳过（Create 先插行后建目录 + AUTOINCREMENT ID 不复用，重查足以防误删活任务）。
func (s *Service) cleanupOrphans(stats *CleanupStats) {
	base := s.cfg.Storage.BaseDir
	framesBase := filepath.Join(base, s.cfg.Storage.FramesDir)
	videosBase := filepath.Join(base, s.cfg.Storage.VideosDir)
	logsBase := filepath.Join(base, "logs")

	s.cleanOrphanInDir(framesBase, stats) // task-{id} 目录 + task-{id}.layers.json
	s.cleanOrphanInDir(videosBase, stats) // task-{id} 目录
	s.cleanOrphanInDir(logsBase, stats)   // task-{id}.log 文件
}

// cleanOrphanInDir 扫描 dir 下形如 task-{id} 的目录/文件，无主则删除。
// 目录（task-{id}）计 OrphanDirs，文件（task-{id}.layers.json / .log）计 OrphanFiles。
func (s *Service) cleanOrphanInDir(dir string, stats *CleanupStats) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[cleanup] 扫描 %s 失败: %v", dir, err)
		}
		return
	}
	for _, e := range entries {
		id, ok := parseTaskID(e.Name())
		if !ok {
			continue
		}
		if s.taskExists(id) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if e.IsDir() {
			if sz, err := dirSize(path); err == nil {
				if err := s.storage.RemoveDir(path); err == nil {
					stats.OrphanDirs++
					stats.FreedBytes += sz
				} else {
					log.Printf("[cleanup] 删除孤儿目录 %s 失败: %v", path, err)
				}
			}
			continue
		}
		if fi, err := os.Stat(path); err == nil {
			if err := s.storage.RemoveFile(path); err == nil {
				stats.OrphanFiles++
				stats.FreedBytes += fi.Size()
			}
		}
	}
}

// taskExists 立即重查任务行是否存在（防误删活任务）。
func (s *Service) taskExists(id int64) bool {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM timelapse_tasks WHERE id=?`, id).Scan(&n)
	return err == nil && n > 0
}

// latestRecordFrameCount 返回某任务最近一条成功视频记录的帧数。
func (s *Service) latestRecordFrameCount(taskID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT frame_count FROM timelapse_records
		WHERE task_id=? AND status=? ORDER BY id DESC LIMIT 1`, taskID, RecordSuccess).Scan(&n)
	return n, err
}

// parseTaskID 解析 task-123 / task-123.layers.json / task-123.log 中的任务 ID。
func parseTaskID(name string) (int64, bool) {
	m := taskDirRe.FindStringSubmatch(name)
	if m == nil {
		m = taskFileRe.FindStringSubmatch(name)
	}
	if m == nil {
		return 0, false
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// dirSize 递归统计目录下所有文件字节数。
func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			total += fi.Size()
		}
		return nil
	})
	return total, err
}
