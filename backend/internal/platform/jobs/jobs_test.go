package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lsy/blog/internal/domain"
)

func TestQueueStatsOldestPendingAge(t *testing.T) {
	now := time.Date(2026, time.July, 18, 12, 0, 0, 0, time.UTC)
	stats := QueueStats{}
	if age := stats.OldestPendingAge(now); age != 0 {
		t.Fatalf("empty queue age = %s, want zero", age)
	}
	oldest := now.Add(-3 * time.Minute)
	stats.OldestPendingAt = &oldest
	if age := stats.OldestPendingAge(now); age != 3*time.Minute {
		t.Fatalf("oldest age = %s, want 3m", age)
	}
}

func TestRegistryHandle(t *testing.T) {
	registry := NewRegistry()
	called := false
	registry.Register("example", func(_ context.Context, job *domain.Job) error {
		called = job.JobType == "example"
		return nil
	})

	if err := registry.Handle(context.Background(), &domain.Job{JobType: "example"}); err != nil {
		t.Fatalf("Handle() error: %v", err)
	}
	if !called {
		t.Fatal("registered handler was not called")
	}
	if err := registry.Handle(context.Background(), &domain.Job{JobType: "missing"}); err == nil {
		t.Fatal("Handle() missing handler succeeded")
	}
}

func TestProducerUsesConfiguredAttempts(t *testing.T) {
	producer := NewProducer(nil, 9)
	if producer.maxAttempts != 9 {
		t.Fatalf("maxAttempts = %d, want 9", producer.maxAttempts)
	}
	fallback := NewProducer(nil, 0)
	if fallback.maxAttempts != 5 {
		t.Fatalf("fallback maxAttempts = %d, want 5", fallback.maxAttempts)
	}
}

func TestEnqueueOptions(t *testing.T) {
	job := &domain.Job{}
	for _, option := range []EnqueueOption{WithPriority(7), WithMaxAttempts(3), WithDedupKey("key")} {
		option(job)
	}
	if job.Priority != 7 || job.MaxAttempts != 3 || job.DedupKey == nil || *job.DedupKey != "key" {
		t.Fatalf("options produced unexpected job: %#v", job)
	}
}

func TestRegistryPropagatesHandlerError(t *testing.T) {
	want := errors.New("failed")
	registry := NewRegistry()
	registry.Register("example", func(context.Context, *domain.Job) error { return want })
	if err := registry.Handle(context.Background(), &domain.Job{JobType: "example"}); !errors.Is(err, want) {
		t.Fatalf("Handle() error = %v, want %v", err, want)
	}
}
