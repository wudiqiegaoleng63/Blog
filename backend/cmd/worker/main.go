// Package main 是 Worker 入口。
//
// 阶段 1：从 background_jobs 表领取并执行任务。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lsy/blog/internal/bootstrap"
	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/domain"
	"github.com/lsy/blog/internal/platform/jobs"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load failed: %v\n", err)
		os.Exit(1)
	}

	if cfg.App.ServiceMode != "worker" {
		fmt.Fprintf(os.Stderr, "APP_SERVICE_MODE must be worker to run worker, got %q\n", cfg.App.ServiceMode)
		os.Exit(1)
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	c, err := bootstrap.New(rootCtx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap failed: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := c.Close(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "shutdown error: %v\n", err)
		}
	}()

	consumer := jobs.NewConsumer(c.DB, cfg.Jobs)
	registry := jobs.NewRegistry()

	// Stage 1: register placeholder handlers for known job types.
	registry.Register("comment_moderation", func(ctx context.Context, job *domain.Job) error {
		// Placeholder — real moderation logic in a future stage.
		return nil
	})
	registry.Register("post_index", func(ctx context.Context, job *domain.Job) error {
		// Placeholder — real indexing in Stage 3.
		return nil
	})

	c.Logger.Info("worker started (stage 1)",
		"env", cfg.App.Env,
		"locked_by", consumer.LockedBy(),
		"poll_interval", consumer.PollInterval().String(),
	)

	poll := time.NewTicker(consumer.PollInterval())
	defer poll.Stop()

	for {
		select {
		case <-rootCtx.Done():
			c.Logger.Info("worker shutting down")
			return
		case <-poll.C:
			claimed, err := consumer.Claim(rootCtx)
			if err != nil {
				c.Logger.Warn("claim jobs failed", "error", err)
				continue
			}

			for _, job := range claimed {
				jobCtx, cancel := context.WithTimeout(rootCtx, 5*time.Minute)
				err := registry.Handle(jobCtx, &job)
				cancel()

				if err != nil {
					c.Logger.Warn("job failed",
						"job_id", job.PublicID,
						"job_type", job.JobType,
						"attempt", job.Attempts,
						"error", err,
					)
					_ = consumer.Fail(context.Background(), job.ID, err.Error())
				} else {
					_ = consumer.Complete(context.Background(), job.ID)
				}
			}

			if len(claimed) > 0 {
				c.Logger.Info("processed jobs", "count", len(claimed))
			}
		}
	}
}
