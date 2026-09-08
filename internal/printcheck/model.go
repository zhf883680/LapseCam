package printcheck

import "time"

// Check 一次 AI 打印健康分析的结果（每次逐层截图后产生一条）。
type Check struct {
	ID         int64     `json:"id"`
	TaskID     int64     `json:"taskId"`
	Status     string    `json:"status"`
	Confidence float64   `json:"confidence"`
	Reason     string    `json:"reason,omitempty"`
	ImagePath  string    `json:"-"`                  // 现场图存储路径（告警时保留）
	ImageURL   string    `json:"imageUrl,omitempty"` // /api/quick/checks/{id}/image
	Alert      bool      `json:"alert"`              // 是否触发告警（发送过 Webhook）
	CreatedAt  time.Time `json:"createdAt"`
}
