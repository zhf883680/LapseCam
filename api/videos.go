package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

func (s *Server) listVideos(w http.ResponseWriter, r *http.Request) {
	recs, err := s.tl.ListRecords()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, recs)
}

func (s *Server) getVideo(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	rec, err := s.tl.GetRecord(id)
	if err != nil {
		if isNotFound(err) {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// getVideoFile 流式返回 MP4 文件，供播放/下载。
func (s *Server) getVideoFile(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	path, err := s.tl.RecordFilePath(id)
	if err != nil {
		if isNotFound(err) {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeErr(w, http.StatusNotFound, "video file not found")
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		writeErr(w, http.StatusNotFound, "video file not found")
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("Content-Disposition", "inline; filename=\""+filepath.Base(path)+"\"")
	http.ServeFile(w, r, path)
}

func (s *Server) deleteVideo(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.tl.DeleteRecord(id); err != nil {
		if isNotFound(err) {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}
