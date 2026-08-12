package timelapse

import "time"

// 任务状态
const (
	StatusPending   = "pending"   // 等待开始（已创建/定时未到）
	StatusRunning   = "running"   // 抽帧中
	StatusStopping  = "stopping"  // 正在停止并生成视频
	StatusCompleted = "completed" // 已完成（视频已生成）
	StatusFailed    = "failed"    // 失败
	StatusStopped   = "stopped"   // 未开始即被手动停止
)

// 视频记录状态
const (
	RecordSuccess = "success"
	RecordFailed  = "failed"
)

// Task 延时摄影任务。
type Task struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	CameraID        int64      `json:"cameraId"`
	CameraName      string     `json:"cameraName,omitempty"`
	IntervalSeconds int        `json:"intervalSeconds"` // 抽帧间隔（秒）
	OutputFPS       int        `json:"outputFps"`       // 成片帧率
	Width           int        `json:"width"`           // 成片宽
	Height          int        `json:"height"`          // 成片高
	StartAt         time.Time  `json:"startAt"`
	EndAt           *time.Time `json:"endAt,omitempty"`
	Status          string     `json:"status"`
	ErrorMessage    string     `json:"errorMessage,omitempty"`
	ActualStartedAt *time.Time `json:"-"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`

	FrameCount int     `json:"frameCount,omitempty"` // 当前已抽帧数
	ProgressPct float64 `json:"progressPct,omitempty"` // 进度百分比（按时间估算）
}

// TaskInput 创建任务的入参。
type TaskInput struct {
	Name            string  `json:"name"`
	CameraID        int64   `json:"cameraId"`
	IntervalSeconds int     `json:"intervalSeconds"`
	OutputFPS       int     `json:"outputFps"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	StartAt         string  `json:"startAt"`          // RFC3339 或 "2006-01-02 15:04:05"
	EndAt           *string `json:"endAt"`            // 可空，为空表示手动停止
}

// Record 一条实际生成的视频记录。
type Record struct {
	ID              int64     `json:"id"`
	TaskID          int64     `json:"taskId"`
	TaskName        string    `json:"taskName,omitempty"`
	StartTime       time.Time `json:"startTime"`
	EndTime         time.Time `json:"endTime"`
	FrameCount      int       `json:"frameCount"`
	DurationSeconds float64   `json:"durationSeconds"`
	FilePath        string    `json:"filePath"`
	FileSize        int64     `json:"fileSize"`
	Status          string    `json:"status"`
	ErrorMessage    string    `json:"errorMessage,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}
