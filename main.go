package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"novelagent/internal/app"
	"novelagent/internal/project"
)

//go:embed web
var webFiles embed.FS

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP 监听地址")
	dataDir := flag.String("data", "data", "作品与设置保存目录")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(*addr, *dataDir, logger); err != nil {
		logger.Error("novel agent stopped", "err", err)
		os.Exit(1)
	}
}

func run(addr, dataDir string, logger *slog.Logger) error {
	staticFS, err := fs.Sub(webFiles, "web")
	if err != nil {
		return fmt.Errorf("load embedded web assets: %w", err)
	}
	projects, err := project.NewManager(dataDir, logger)
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := projects.Start(ctx); err != nil {
		return fmt.Errorf("start projects: %w", err)
	}
	defer projects.Stop()

	httpServer := &http.Server{
		Handler:           app.NewRouter(projects, staticFS, logger),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- httpServer.Serve(listener)
	}()

	logger.Info("novel agent is ready", "url", "http://"+listener.Addr().String(), "data", dataDir)

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	logger.Info("novel agent stopped")
	return nil
}
