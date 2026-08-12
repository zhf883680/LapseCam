package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cam.StartChecker(ctx)
	tl.StartScheduler(ctx)

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           api.New(cfg, cam, tl, st).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("timelapse server listening on %s", cfg.Server.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Println("bye")
}
