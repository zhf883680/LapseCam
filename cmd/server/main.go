package main

import (
	"context"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"timelapse/api"
	"timelapse/config"
	"timelapse/internal/camera"
	"timelapse/internal/database"
	"timelapse/internal/ffmpeg"
	"timelapse/internal/storage"
	"timelapse/internal/timelapse"
)

func main() {
	cfgPath := flag.String("config", "config/config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Printf("config load failed (%v), using defaults", err)
		cfg = config.Default()
	}

	db, err := database.Open(cfg.Database.Path)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	st := storage.New(cfg)
	ff := ffmpeg.New(cfg)
	cam := camera.New(db, cfg, ff)
	tl := timelapse.New(db, cfg, ff, cam, st)

	// 初始化日志：终端 + 日志文件双输出，方便 systemd/前台/远程排查
	if err := st.EnsureDir(cfg.Storage.BaseDir); err != nil {
		log.Printf("ensure data dir %s: %v", cfg.Storage.BaseDir, err)
	}
	logPath := filepath.Join(cfg.Storage.BaseDir, "lapsecam.log")
	if lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err != nil {
		log.Printf("open log file %s failed: %v", logPath, err)
	} else {
		defer lf.Close()
		log.SetOutput(io.MultiWriter(os.Stdout, lf))
		log.Printf("日志文件: %s", logPath)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cam.StartChecker(ctx)
	tl.StartScheduler(ctx)

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           api.New(cfg, cam, tl, st).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// 启动 banner：同步打印，确保启动即有明确反馈
	log.Println("================================")
	log.Println("  LapseCam 延时摄影服务")
	log.Println("================================")
	log.Printf("  HTTP API   : http://0.0.0.0%s", cfg.Server.Addr)
	log.Printf("  数据目录   : %s", cfg.Storage.BaseDir)
	log.Printf("  数据库     : %s", cfg.Database.Path)
	log.Printf("  ffmpeg     : %s", cfg.FFmpeg.Binary)
	log.Printf("  ffprobe    : %s", cfg.FFmpeg.FFProbe)
	log.Printf("  编码参数   : preset=%s crf=%d", cfg.FFmpeg.EncodePreset, cfg.FFmpeg.EncodeCRF)
	log.Println("================================")
	log.Println("服务已启动，等待任务...")

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Println("shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		log.Println("bye")
	case err := <-errCh:
		log.Fatalf("server error: %v", err)
	}
}
