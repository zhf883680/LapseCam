package api

import (
	"errors"
	"net/http"

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
