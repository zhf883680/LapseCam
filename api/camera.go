package api

import (
	"errors"
	"net/http"

	"timelapse/internal/camera"
)

func (s *Server) listCameras(w http.ResponseWriter, r *http.Request) {
	cams, err := s.cam.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cams)
}

func (s *Server) createCamera(w http.ResponseWriter, r *http.Request) {
	var in camera.CameraInput
	if err := readJSON(w, r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	c, err := s.cam.Create(in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) updateCamera(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in camera.CameraInput
	if err := readJSON(w, r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	c, err := s.cam.Update(id, in)
	if err != nil {
		if isNotFound(err) {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) deleteCamera(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	active, err := s.tl.HasActiveTasks(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if active {
		writeErr(w, http.StatusConflict, "camera is used by active timelapse tasks, stop or delete them first")
		return
	}
	if err := s.cam.Delete(id); err != nil {
		if isNotFound(err) {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (s *Server) testCamera(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	res, err := s.cam.Test(id)
	if err != nil {
		if errors.Is(err, camera.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !res.OK {
		// 连接失败也返回 200，把 ok=false 交给前端展示
		writeJSON(w, http.StatusOK, res)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
