// Package main 是 API 服务入口。负责加载配置、装配依赖并启动 HTTP。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/lsy/blog/internal/bootstrap"
	"github.com/lsy/blog/internal/config"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load failed: %v\n", err)
		return 1
	}
	if cfg.App.ServiceMode != "api" {
		fmt.Fprintf(os.Stderr, "APP_SERVICE_MODE must be api to run api, got %q\n", cfg.App.ServiceMode)
		return 1
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	c, err := bootstrap.New(rootCtx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap failed: %v\n", err)
		return 1
	}
	defer func() {
		if err := c.Close(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "shutdown error: %v\n", err)
		}
	}()

	if err := c.ServeAPI(rootCtx); err != nil {
		fmt.Fprintf(os.Stderr, "api serve failed: %v\n", err)
		return 1
	}
	return 0
}
