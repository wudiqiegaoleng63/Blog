// Package jobs implements the MySQL-backed background job queue.
//
// Producers call Enqueue to atomically insert a job. Workers call Claim and
// Complete/Fail to process jobs with at-least-once semantics.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/domain"
	"github.com/lsy/blog/internal/platform/ids"
)

// Producer enqueues jobs into the database.
type Producer struct {
	db *gorm.DB
}

func NewProducer(db *gorm.DB) *Producer {
	return &Producer{db: db}
}

// Enqueue inserts a new job. The call should be wrapped in the caller's
// business transaction when atomicity with domain writes is required.
func (p *Producer) Enqueue(ctx context.Context, tx *gorm.DB, jobType string, payload interface{}, opts ...EnqueueOption) (*domain.Job, error) {
	db := tx
	if db == nil {
		db = p.db
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("jobs: marshal payload: %w", err)
	}

	now := time.Now().UTC()
	job := &domain.Job{
		PublicID:    ids.MustNewULID(),
		JobType:     jobType,
		Status:      "pending",
		Priority:    0,
		MaxAttempts: 5,
		RunAfter:    now,
		PayloadJSON: payloadJSON,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	for _, opt := range opts {
		opt(job)
	}

	if err := db.WithContext(ctx).Create(job).Error; err != nil {
		return nil, fmt.Errorf("jobs: enqueue: %w", err)
	}
	return job, nil
}

type EnqueueOption func(*domain.Job)

func WithPriority(p int) EnqueueOption    { return func(j *domain.Job) { j.Priority = p } }
func WithMaxAttempts(n int) EnqueueOption { return func(j *domain.Job) { j.MaxAttempts = n } }
func WithRunAfter(t time.Time) EnqueueOption { return func(j *domain.Job) { j.RunAfter = t } }
func WithDedupKey(k string) EnqueueOption {
	return func(j *domain.Job) { j.DedupKey = &k }
}

// Consumer polls and claims jobs from the database.
type Consumer struct {
	db          *gorm.DB
	lockedBy    string
	pollInterval time.Duration
	lockSeconds  int
	batchSize    int
}

func NewConsumer(db *gorm.DB, cfg config.JobsConfig) *Consumer {
	return &Consumer{
		db:           db,
		lockedBy:     ids.MustNewULID(),
		pollInterval: cfg.PollInterval,
		lockSeconds:  cfg.LockSeconds,
		batchSize:    cfg.BatchSize,
	}
}

// Claim attempts to lock up to batchSize pending jobs using
// SELECT ... FOR UPDATE SKIP LOCKED for concurrency safety.
func (c *Consumer) Claim(ctx context.Context) ([]domain.Job, error) {
	var jobs []domain.Job
	now := time.Now().UTC()

	err := c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// MySQL 8.0+ supports SKIP LOCKED
		return tx.Raw(`
			SELECT id, public_id, job_type, payload_json, status, priority,
			       attempts, max_attempts, run_after, locked_by, locked_at,
			       last_error, created_at, updated_at, finished_at
			FROM background_jobs
			WHERE status = 'pending'
			  AND run_after <= ?
			ORDER BY priority DESC, created_at ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED
		`, now, c.batchSize).Scan(&jobs).Error
	})
	if err != nil {
		return nil, fmt.Errorf("jobs: claim: %w", err)
	}

	// Mark claimed jobs as running.
	for i := range jobs {
		jobs[i].Status = "running"
		jobs[i].LockedBy = &c.lockedBy
		jobs[i].LockedAt = &now
		jobs[i].Attempts++
		jobs[i].UpdatedAt = now

		c.db.WithContext(ctx).
			Model(&domain.Job{}).
			Where("id = ? AND status = 'pending'", jobs[i].ID).
			Updates(map[string]interface{}{
				"status":    "running",
				"locked_by": c.lockedBy,
				"locked_at": now,
				"attempts":  jobs[i].Attempts,
				"updated_at": now,
			})
	}

	return jobs, nil
}

// Complete marks a job as successfully completed.
func (c *Consumer) Complete(ctx context.Context, jobID uint64) error {
	now := time.Now().UTC()
	return c.db.WithContext(ctx).
		Model(&domain.Job{}).
		Where("id = ? AND locked_by = ?", jobID, c.lockedBy).
		Updates(map[string]interface{}{
			"status":      "completed",
			"finished_at": now,
			"updated_at":  now,
		}).Error
}

// Fail marks a job as failed, retrying if attempts remain.
func (c *Consumer) Fail(ctx context.Context, jobID uint64, lastError string) error {
	var job domain.Job
	err := c.db.WithContext(ctx).Where("id = ? AND locked_by = ?", jobID, c.lockedBy).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("jobs: fail: %w", err)
	}

	now := time.Now().UTC()
	errMsg := lastError
	if len(errMsg) > 1000 {
		errMsg = errMsg[:1000]
	}

	if job.Attempts >= job.MaxAttempts {
		return c.db.WithContext(ctx).
			Model(&job).
			Updates(map[string]interface{}{
				"status":      "dead",
				"last_error":  errMsg,
				"finished_at": now,
				"updated_at":  now,
			}).Error
	}

	// Retry: back to pending, schedule after a brief delay.
	retryAfter := now.Add(time.Duration(job.Attempts) * 30 * time.Second)
	return c.db.WithContext(ctx).
		Model(&job).
		Updates(map[string]interface{}{
			"status":     "pending",
			"locked_by":  nil,
			"locked_at":  nil,
			"last_error": errMsg,
			"run_after":  retryAfter,
			"updated_at": now,
		}).Error
}

// PollInterval returns the configured poll interval.
func (c *Consumer) PollInterval() time.Duration { return c.pollInterval }

// LockedBy returns this consumer's unique worker ID.
func (c *Consumer) LockedBy() string { return c.lockedBy }

// ReapStaleJobs releases locks held by dead workers.
func (c *Consumer) ReapStaleJobs(ctx context.Context) error {
	cutoff := time.Now().UTC().Add(-time.Duration(c.lockSeconds) * time.Second)
	return c.db.WithContext(ctx).
		Model(&domain.Job{}).
		Where("status = 'running' AND locked_at < ?", cutoff).
		Updates(map[string]interface{}{
			"status":    "pending",
			"locked_by": nil,
			"locked_at": nil,
			"updated_at": time.Now().UTC(),
		}).Error
}

// --- Job handler registry ---

// Handler is a function that processes a job and returns an error if it fails.
type Handler func(ctx context.Context, job *domain.Job) error

// Registry maps job types to their handlers.
type Registry struct {
	handlers map[string]Handler
}

func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]Handler)}
}

func (r *Registry) Register(jobType string, handler Handler) {
	r.handlers[jobType] = handler
}

func (r *Registry) Handle(ctx context.Context, job *domain.Job) error {
	h, ok := r.handlers[job.JobType]
	if !ok {
		return fmt.Errorf("jobs: no handler registered for %q", job.JobType)
	}
	return h(ctx, job)
}
