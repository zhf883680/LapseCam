package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"timelapse/config"
	"timelapse/internal/camera"
	"timelapse/internal/storage"
	"timelapse/internal/timelapse"
)

// Server 组合各业务服务并注册 HTTP 路由。
type Server struct {
	cfg     *config.Config
	cam     *camera.Service
	tl      *timelapse.Service
	storage *storage.Service
}

func New(cfg *config.Config, cam *camera.Service, tl *timelapse.Service, st *storage.Service) *Server {
	return &Server{cfg: cfg, cam: cam, tl: tl, storage: st}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// 摄像头
	mux.HandleFunc("GET /api/cameras", s.listCameras)
	mux.HandleFunc("POST /api/cameras", s.createCamera)
	mux.HandleFunc("PUT /api/cameras/{id}", s.updateCamera)
	mux.HandleFunc("DELETE /api/cameras/{id}", s.deleteCamera)
	mux.HandleFunc("POST /api/cameras/{id}/test", s.testCamera)

	// 延时摄影任务
	mux.HandleFunc("GET /api/timelapse", s.listTasks)
	mux.HandleFunc("POST /api/timelapse", s.createTask)
	mux.HandleFunc("GET /api/timelapse/{id}", s.getTask)
	mux.HandleFunc("POST /api/timelapse/{id}/start", s.startTask)
	mux.HandleFunc("POST /api/timelapse/{id}/stop", s.stopTask)
	mux.HandleFunc("DELETE /api/timelapse/{id}", s.deleteTask)

	// 视频
	mux.HandleFunc("GET /api/videos", s.listVideos)
	mux.HandleFunc("GET /api/videos/{id}", s.getVideo)
	mux.HandleFunc("GET /api/videos/{id}/file", s.getVideoFile)
	mux.HandleFunc("DELETE /api/videos/{id}", s.deleteVideo)

	// 健康检查
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return s.logMiddleware(s.corsMiddleware(mux))
}

// ---- 通用工具 ----

type envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Code: 0, Message: "ok", Data: data})
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Code: status, Message: msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func isNotFound(err error) bool {
	return errors.Is(err, camera.ErrNotFound) || errors.Is(err, timelapse.ErrNotFound)
}

// ---- 中间件 ----

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
