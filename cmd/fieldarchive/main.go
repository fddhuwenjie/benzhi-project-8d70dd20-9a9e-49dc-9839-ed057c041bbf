package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const defaultAddr = "127.0.0.1:19081"

type config struct {
	addr, dataDir string
	selfCheck     bool
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Printf("fieldarchive: %v", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseConfig(args, os.Getenv("PORT"))
	if err != nil {
		return err
	}
	if cfg.selfCheck {
		return runSelfCheck(cfg)
	}
	app, err := buildApp(cfg.dataDir)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return err
	}
	httpServer := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.Serve(listener) }()
	log.Printf("田野语音档案服务监听 http://%s，数据目录 %s", cfg.addr, cfg.dataDir)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	select {
	case sig := <-signals:
		log.Printf("收到 %s，准备停止", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		return httpServer.Shutdown(ctx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
