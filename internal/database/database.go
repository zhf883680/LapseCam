package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Open 打开（必要时创建）SQLite 数据库并执行迁移。
// 使用纯 Go 的 modernc sqlite 驱动，无需 CGO，方便打进 Alpine 镜像。
func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create database dir: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite 同一时刻只允许一个写者；Go 侧串行化连接避免锁竞争。
	db.SetMaxOpenConns(1)

	// 提升并发小事务下的稳定性
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		return nil, fmt.Errorf("set pragma: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS cameras (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT    NOT NULL,
    rtsp_url     TEXT    NOT NULL,
    username     TEXT    NOT NULL DEFAULT '',
    password     TEXT    NOT NULL DEFAULT '',
    enabled      INTEGER NOT NULL DEFAULT 1,
    online       INTEGER NOT NULL DEFAULT 0,
    last_seen_at TEXT,
    created_at   TEXT    NOT NULL,
    updated_at   TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS timelapse_tasks (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    name             TEXT    NOT NULL,
    camera_id        INTEGER NOT NULL,
    interval_seconds INTEGER NOT NULL,
    output_fps       INTEGER NOT NULL,
    width            INTEGER NOT NULL,
    height           INTEGER NOT NULL,
    start_at         TEXT    NOT NULL,
    end_at           TEXT,
    status           TEXT    NOT NULL DEFAULT 'pending',
    error_message    TEXT    NOT NULL DEFAULT '',
    actual_started_at TEXT,
    created_at       TEXT    NOT NULL,
    updated_at       TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS timelapse_records (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id          INTEGER NOT NULL,
    start_time       TEXT    NOT NULL,
    end_time         TEXT    NOT NULL,
    frame_count      INTEGER NOT NULL,
    duration_seconds REAL    NOT NULL,
    file_path        TEXT    NOT NULL,
    file_size        INTEGER NOT NULL,
    status           TEXT    NOT NULL,
    error_message    TEXT    NOT NULL DEFAULT '',
    created_at       TEXT    NOT NULL,
    updated_at       TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_status   ON timelapse_tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_camera   ON timelapse_tasks(camera_id);
CREATE INDEX IF NOT EXISTS idx_records_task   ON timelapse_records(task_id);
`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// NowUTC 返回统一用于落库的时间（RFC3339 UTC 字符串）。
func NowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// ParseTime 解析 RFC3339 时间字符串，失败时返回零值。
func ParseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}
