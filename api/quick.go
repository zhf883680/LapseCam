package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"

	"timelapse/internal/timelapse"
)

// quickStart 开始快捷录制（供 HA 等外部自动化调用）。
func (s *Server) quickStart(w http.ResponseWriter, r *http.Request) {
	t, already, err := s.tl.QuickStart()
	if err != nil {
		if errors.Is(err, timelapse.ErrNoCamera) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	msg := "已开始录制"
	if already {
		msg = "已在录制"
	}
	writeJSON(w, http.StatusOK, map[string]any{"taskId": t.ID, "status": t.Status, "message": msg})
}

// quickStop 停止当前快捷录制并出片（幂等）。
func (s *Server) quickStop(w http.ResponseWriter, r *http.Request) {
	t, found, err := s.tl.QuickStop()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, map[string]any{"message": "当前没有正在录制的快捷任务"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"taskId": t.ID, "status": t.Status})
}

// quickSnapshot 逐层截图：为当前快捷任务抓取一帧（captureMode=layer）。
// ?layer=N 可选，用于按层幂等防抖（同一层只截一次）。
// 截完自动触发 AI 打印健康分析（后台执行，不阻塞响应）：
// 把该任务最近 vision.analyzeFrames 张帧一起发给视觉模型判断。
func (s *Server) quickSnapshot(w http.ResponseWriter, r *http.Request) {
	layer := 0
	if v := r.URL.Query().Get("layer"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeErr(w, http.StatusBadRequest, "layer must be a positive integer")
			return
		}
		layer = n
	}

	res, err := s.tl.QuickSnapshot(layer)
	if err != nil {
		if errors.Is(err, timelapse.ErrNoQuickTask) || errors.Is(err, timelapse.ErrNotLayerMode) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 逐层截图成功 → 后台分析最近 N 张。仅当该任务已手动开启 AI 检测时才分析
	// （默认关：vision.aiEnabledByDefault=false，需在「AI 监控」页对当前打印任务开启）
	if res.Captured && s.pc != nil {
		taskID := res.TaskID
		if enabled, aerr := s.tl.TaskAIEnabled(taskID); aerr == nil && enabled {
			go func() {
				if err := s.pc.CheckRecent(context.Background(), taskID); err != nil {
					log.Printf("[printcheck] analyze task %d failed: %v", taskID, err)
				}
			}()
		} else if aerr != nil {
			log.Printf("[quick] task %d ai_enabled query failed: %v", taskID, aerr)
		}
	}

	msg := "已截图"
	if !res.Captured {
		msg = "该层已截图，跳过"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"taskId":   res.TaskID,
		"layer":    res.Layer,
		"frame":    res.Frame,
		"captured": res.Captured,
		"message":  msg,
	})
}

// quickCheck 返回当前打印任务最近一次 AI 分析结果（无任务/无记录时 found=false）。
func (s *Server) quickCheck(w http.ResponseWriter, r *http.Request) {
	taskID := s.tl.ActiveQuickTaskID()
	if taskID == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"found": false})
		return
	}
	c, found, err := s.pc.Latest(taskID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, map[string]any{"found": false, "taskId": taskID})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": true, "check": c})
}

// quickChecks 列出分析历史。?taskId= 指定任务（缺省用当前快捷任务），?limit= 条数。
func (s *Server) quickChecks(w http.ResponseWriter, r *http.Request) {
	taskID, _ := strconv.ParseInt(r.URL.Query().Get("taskId"), 10, 64)
	if taskID == 0 {
		taskID = s.tl.ActiveQuickTaskID()
	}
	if taskID == 0 {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	checks, err := s.pc.List(taskID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, checks)
}

// quickCheckImage 返回某次分析的现场图（告警时保留）。
func (s *Server) quickCheckImage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	path, err := s.pc.CheckImagePath(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "check image not found")
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		writeErr(w, http.StatusNotFound, "check image not found")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	http.ServeFile(w, r, path)
}

// quickLayer 记录层变化时间戳（captureMode=timestamp 出片选帧用）。
// ?layer=N 可选，仅作记录便于排查。
func (s *Server) quickLayer(w http.ResponseWriter, r *http.Request) {
	layer := 0
	if v := r.URL.Query().Get("layer"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeErr(w, http.StatusBadRequest, "layer must be a positive integer")
			return
		}
		layer = n
	}

	res, err := s.tl.QuickRecordLayer(layer)
	if err != nil {
		if errors.Is(err, timelapse.ErrNoQuickTask) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"taskId":   res.TaskID,
		"layer":    res.Layer,
		"recorded": res.Recorded,
		"message":  "已记录层变化",
	})
}

// listAllChecks 返回跨任务的 AI 分析审计记录（?limit= 条数，默认 100）。
func (s *Server) listAllChecks(w http.ResponseWriter, r *http.Request) {
	if s.pc == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	checks, err := s.pc.ListAll(limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, checks)
}

// quickTaskInfo 返回当前快捷任务信息（含 AI 检测开关状态）。
func (s *Server) quickTaskInfo(w http.ResponseWriter, r *http.Request) {
	id := s.tl.ActiveQuickTaskID()
	if id == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"taskId": 0, "aiEnabled": false})
		return
	}
	aiEnabled, err := s.tl.TaskAIEnabled(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	t, err := s.tl.Get(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"taskId": id, "aiEnabled": aiEnabled, "name": t.Name, "status": t.Status,
		"message": "任务 AI 检测：默认关闭，需手动开启",
	})
}

// quickSetAI 设置当前快捷任务是否开启 AI 检测。
func (s *Server) quickSetAI(w http.ResponseWriter, r *http.Request) {
	id := s.tl.ActiveQuickTaskID()
	if id == 0 {
		writeErr(w, http.StatusBadRequest, "没有正在录制的快捷任务")
		return
	}
	var in struct {
		Enabled *bool `json:"enabled"`
	}
	if err := readJSON(w, r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Enabled == nil {
		writeErr(w, http.StatusBadRequest, "enabled 必填")
		return
	}
	if err := s.tl.SetAIEnabled(id, *in.Enabled); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"taskId": id, "aiEnabled": *in.Enabled})
}
