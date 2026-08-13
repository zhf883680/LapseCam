package api

import (
	"errors"
	"net/http"

	"timelapse/internal/timelapse"
)

// cleanup 手动触发一次数据清理。
func (s *Server) cleanup(w http.ResponseWriter, r *http.Request) {
	stats, err := s.tl.Cleanup()
	if err != nil {
		if errors.Is(err, timelapse.ErrCleanupInProgress) {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
