// Package main 是 Worker 入口。
//
// 阶段 1：从 background_jobs 表领取并执行任务。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/lsy/blog/internal/bootstrap"
	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/domain"
	aimod "github.com/lsy/blog/internal/modules/ai"
	"github.com/lsy/blog/internal/modules/comments"
	"github.com/lsy/blog/internal/platform/jobs"
	"github.com/lsy/blog/internal/platform/openaicompat"
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
	commentsRepo := comments.NewRepository(c.DB, cfg.Jobs.MaxAttempts)
	registry.Register(comments.ModerationJobType, func(ctx context.Context, job *domain.Job) error {
		return commentsRepo.Moderate(ctx, job.PayloadJSON)
	})
	if cfg.AI.IndexingEnabled {
		embedder, err := openaicompat.New(cfg.AI.Embedding.BaseURL, cfg.AI.Embedding.APIKey, cfg.AI.Embedding.Timeout, cfg.AI.Embedding.MaxRetries)
		if err != nil {
			fmt.Fprintf(os.Stderr, "embedding client setup failed: %v\n", err)
			os.Exit(1)
		}
		vectors := aimod.NewMilvusStore(cfg.Milvus, cfg.AI.Embedding.Dimensions)
		defer vectors.Close(context.Background())
		indexer := aimod.NewIndexer(aimod.NewRepository(c.DB, cfg.Jobs.MaxAttempts), embedder, vectors, cfg.AI)
		registry.Register(aimod.IndexJobType, func(ctx context.Context, job *domain.Job) error {
			return indexer.Handle(ctx, job.PayloadJSON)
		})
	}

	c.Logger.Info("worker started",
		"env", cfg.App.Env,
		"locked_by", consumer.LockedBy(),
		"poll_interval", consumer.PollInterval().String(),
	)

	poll := time.NewTicker(consumer.PollInterval())
	defer poll.Stop()
	reapInterval := time.Duration(cfg.Jobs.LockSeconds) * time.Second / 2
	if reapInterval < consumer.PollInterval() {
		reapInterval = consumer.PollInterval()
	}
	reap := time.NewTicker(reapInterval)
	defer reap.Stop()

	if err := consumer.ReapStaleJobs(rootCtx); err != nil {
		c.Logger.Warn("initial stale job recovery failed", "error", err)
	}

	for {
		select {
		case <-rootCtx.Done():
			c.Logger.Info("worker shutting down")
			return
		case <-reap.C:
			if err := consumer.ReapStaleJobs(rootCtx); err != nil {
				c.Logger.Warn("stale job recovery failed", "error", err)
			}
		case <-poll.C:
			claimed, err := consumer.Claim(rootCtx)
			if err != nil {
				c.Logger.Warn("claim jobs failed", "error", err)
				continue
			}

			var batch sync.WaitGroup
			for i := range claimed {
				job := claimed[i]
				batch.Add(1)
				go func() {
					defer batch.Done()
					// Finish or fail a handler before its lease can be reaped. Longer job
					// types must add lease renewal before increasing this timeout.
					jobTimeout := time.Duration(cfg.Jobs.LockSeconds) * time.Second / 2
					jobCtx, cancel := context.WithTimeout(rootCtx, jobTimeout)
					err := registry.Handle(jobCtx, &job)
					cancel()

					if err != nil {
						c.Logger.Warn("job failed",
							"job_id", job.PublicID,
							"job_type", job.JobType,
							"attempt", job.Attempts,
							"error", err,
						)
						if failErr := consumer.Fail(context.Background(), job.ID, err.Error()); failErr != nil {
							c.Logger.Error("record job failure failed", "job_id", job.PublicID, "error", failErr)
						}
					} else if completeErr := consumer.Complete(context.Background(), job.ID); completeErr != nil {
						c.Logger.Error("complete job failed", "job_id", job.PublicID, "error", completeErr)
					}
				}()
			}
			batch.Wait()

			if len(claimed) > 0 {
				c.Logger.Info("processed jobs", "count", len(claimed))
			}
		}
	}
}
