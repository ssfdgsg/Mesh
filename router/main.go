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

	"router/internal/controlplane"
	"router/internal/sidercfg"
)

func main() {
	var addr string
	var cfgPath string
	var pollInterval time.Duration
	var uiDir string

	flag.StringVar(&addr, "addr", ":8081", "http listen address")
	flag.StringVar(&cfgPath, "config", "./sider.config.json", "sider config file path (json)")
	flag.DurationVar(&pollInterval, "poll", 1*time.Second, "config file poll interval")
	flag.StringVar(&uiDir, "ui", "", "optional UI static dir (e.g. ../web/dist)")
	flag.Parse()

	cfgLoader := sidercfg.FileLoader{Path: cfgPath}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv := &http.Server{
		Addr:    addr,
		Handler: controlplane.NewMux(ctx, cfgLoader, pollInterval, uiDir),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutdownCancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	uiStatus := uiDir
	if uiStatus == "" {
		uiStatus = "disabled"
	}
	log.Printf("router start: addr=%s config=%s poll=%s ui=%s", addr, cfgPath, pollInterval, uiStatus)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("router exit")
}
