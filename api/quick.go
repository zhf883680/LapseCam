package api

import (
	"errors"
	"net/http"
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
