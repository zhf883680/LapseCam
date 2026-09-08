package printcheck

import (
	"time"

	"timelapse/internal/vision"
)

// decideAlert 判定这次异常是否应该发告警：
//   - 只在「连续异常次数刚好达到 failureStreak」的上升沿发（故障持续时不重复轰炸）
//   - 若距上次告警仍在冷却期内则不重复发（防抖动快速恢复又复发时刷屏）
//
// 纯函数，便于单测。
func decideAlert(failureStreak, streak int, lastAlertAt time.Time, cooldown time.Duration, now time.Time) bool {
	if streak != failureStreak {
		return false
	}
	if lastAlertAt.IsZero() {
		return true
	}
	return now.Sub(lastAlertAt) >= cooldown
}

// isAbnormal 按策略判断一次结果是否算异常（AI 状态属于异常集合且置信度达标）。
func isAbnormal(status string, confidence, minConfidence float64) bool {
	if confidence < minConfidence {
		return false
	}
	switch status {
	case vision.StatusSpaghetti, vision.StatusClog, vision.StatusObjectDisplaced,
		vision.StatusNozzleCollision, vision.StatusMaterialBuildup:
		return true
	}
	return false
}
