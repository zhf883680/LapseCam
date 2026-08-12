package timelapse

import (
	"database/sql"
	"errors"
	"fmt"

	"timelapse/internal/database"
)

// ListRecords 返回全部视频记录（按生成时间倒序）。
func (s *Service) ListRecords() ([]Record, error) {
	rows, err := s.db.Query(`SELECT r.id, r.task_id, t.name, r.start_time, r.end_time, r.frame_count,
		r.duration_seconds, r.file_path, r.file_size, r.status, r.error_message, r.created_at, r.updated_at
		FROM timelapse_records r LEFT JOIN timelapse_tasks t ON t.id = r.task_id
		ORDER BY r.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Record, 0)
	for rows.Next() {
		var r Record
		if err := scanRecord(rows, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRecord 返回单条视频记录。
func (s *Service) GetRecord(id int64) (Record, error) {
	row := s.db.QueryRow(`SELECT r.id, r.task_id, t.name, r.start_time, r.end_time, r.frame_count,
		r.duration_seconds, r.file_path, r.file_size, r.status, r.error_message, r.created_at, r.updated_at
		FROM timelapse_records r LEFT JOIN timelapse_tasks t ON t.id = r.task_id
		WHERE r.id=?`, id)
	var r Record
	err := scanRecord(row, &r)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	return r, err
}

// DeleteRecord 删除单条视频记录及其文件。
func (s *Service) DeleteRecord(id int64) error {
	r, err := s.GetRecord(id)
	if err != nil {
		return err
	}
	if r.FilePath != "" {
		_ = s.storage.RemoveFile(r.FilePath)
	}
	if _, err := s.db.Exec(`DELETE FROM timelapse_records WHERE id=?`, id); err != nil {
		return err
	}
	return nil
}

// RecordFilePath 返回视频文件绝对/相对路径（供下载接口使用）。
func (s *Service) RecordFilePath(id int64) (string, error) {
	r, err := s.GetRecord(id)
	if err != nil {
		return "", err
	}
	if r.FilePath == "" {
		return "", fmt.Errorf("record %d has no file", id)
	}
	return r.FilePath, nil
}

func (s *Service) listRecordsByTask(taskID int64) ([]Record, error) {
	rows, err := s.db.Query(`SELECT id, task_id, '', start_time, end_time, frame_count,
		duration_seconds, file_path, file_size, status, error_message, created_at, updated_at
		FROM timelapse_records WHERE task_id=?`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Record, 0)
	for rows.Next() {
		var r Record
		if err := scanRecord(rows, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(...any) error
}

func scanRecord(r scanner, rec *Record) error {
	var start, end, createdAt, updatedAt string
	var taskName sql.NullString
	if err := r.Scan(&rec.ID, &rec.TaskID, &taskName, &start, &end, &rec.FrameCount,
		&rec.DurationSeconds, &rec.FilePath, &rec.FileSize, &rec.Status, &rec.ErrorMessage,
		&createdAt, &updatedAt); err != nil {
		return err
	}
	rec.TaskName = taskName.String
	rec.StartTime = database.ParseTime(start)
	rec.EndTime = database.ParseTime(end)
	rec.CreatedAt = database.ParseTime(createdAt)
	rec.UpdatedAt = database.ParseTime(updatedAt)
	return nil
}
