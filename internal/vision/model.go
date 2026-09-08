package vision

// 检测状态。AI 只负责把一组截图归类，是否报警由调用方按规则判定。
const (
	StatusNormal          = "normal"           // 正常
	StatusSpaghetti       = "spaghetti"        // 炒面：挤出丝乱成一团，不再成型
	StatusClog            = "clog"             // 堵头：喷嘴堵塞 / 严重积料不再正常出料
	StatusObjectDisplaced = "object_displaced" // 打印件/支撑脱离原位、翘起或被喷嘴拖着走
	StatusNozzleCollision = "nozzle_collision" // 喷嘴撞到打印件/异物
	StatusMaterialBuildup = "material_buildup" // 喷嘴周围明显积料
	StatusUnknown         = "unknown"          // 无法判断
)

// AnalysisResult 一次分析（一组图）的结构化结果。
type AnalysisResult struct {
	Status     string  `json:"status"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

// IsAbnormal 该状态是否属于打印异常。
func (r *AnalysisResult) IsAbnormal() bool {
	switch r.Status {
	case StatusSpaghetti, StatusClog, StatusObjectDisplaced, StatusNozzleCollision, StatusMaterialBuildup:
		return true
	}
	return false
}

// KnownStatus 校验状态名是否合法。
func KnownStatus(s string) bool {
	switch s {
	case StatusNormal, StatusSpaghetti, StatusClog, StatusObjectDisplaced, StatusNozzleCollision, StatusMaterialBuildup, StatusUnknown:
		return true
	}
	return false
}
