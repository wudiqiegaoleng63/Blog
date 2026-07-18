//go:build integration

package jobs

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/platform/database"
	"github.com/lsy/blog/internal/platform/migrations"
)

func TestMySQLConcurrentClaimAndLifecycle(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Fatal("TEST_MYSQL_DSN is required for integration tests")
	}
	if err := migrations.RunUp(dsn); err != nil {
		t.Fatalf("RunUp(first): %v", err)
	}
	if err := migrations.RunUp(dsn); err != nil {
		t.Fatalf("RunUp(second): %v", err)
	}
	version, dirty, err := migrations.Version(dsn)
	if err != nil {
		t.Fatalf("Version(): %v", err)
	}
	versions, err := migrations.ListVersions()
	if err != nil {
		t.Fatalf("ListVersions(): %v", err)
	}
	if len(versions) == 0 || version != versions[len(versions)-1] || dirty {
		t.Fatalf("migration state = version %d dirty %v, want latest %v clean", version, dirty, versions)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.New(ctx, config.MySQLConfig{
		DSN: dsn, MaxOpenConns: 10, MaxIdleConns: 5,
		ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute,
	}, "dev")
	if err != nil {
		t.Fatalf("database.New(): %v", err)
	}
	defer database.Close(db)

	if err := db.Exec("DELETE FROM background_jobs WHERE job_type = ?", "integration_claim").Error; err != nil {
		t.Fatalf("clean jobs: %v", err)
	}
	producer := NewProducer(db, 3)
	const jobCount = 12
	runAfter := time.Now().UTC().Add(-time.Second)
	for i := 0; i < jobCount; i++ {
		key := fmt.Sprintf("integration-claim-%02d", i)
		if _, err := producer.Enqueue(ctx, "integration_claim", map[string]int{"index": i}, WithDedupKey(key), WithRunAfter(runAfter)); err != nil {
			t.Fatalf("Enqueue(%d): %v", i, err)
		}
	}

	cfg := config.JobsConfig{PollInterval: time.Millisecond, LockSeconds: 30, BatchSize: jobCount / 2}
	first := NewConsumer(db, cfg)
	second := NewConsumer(db, cfg)
	start := make(chan struct{})
	type result struct {
		consumer *Consumer
		jobs     []uint64
		err      error
	}
	results := make(chan result, 2)
	claim := func(consumer *Consumer) {
		<-start
		claimed, err := consumer.Claim(ctx)
		ids := make([]uint64, 0, len(claimed))
		for _, job := range claimed {
			ids = append(ids, job.ID)
		}
		results <- result{consumer: consumer, jobs: ids, err: err}
	}
	go claim(first)
	go claim(second)
	close(start)

	seen := make(map[uint64]struct{}, jobCount)
	for range 2 {
		claimed := <-results
		if claimed.err != nil {
			t.Fatalf("Claim(): %v", claimed.err)
		}
		for _, id := range claimed.jobs {
			if _, duplicate := seen[id]; duplicate {
				t.Fatalf("job %d claimed by more than one worker", id)
			}
			seen[id] = struct{}{}
			if err := claimed.consumer.Complete(ctx, id); err != nil {
				t.Fatalf("Complete(%d): %v", id, err)
			}
		}
	}
	if len(seen) != jobCount {
		t.Fatalf("claimed %d unique jobs, want %d", len(seen), jobCount)
	}

	var completed int64
	if err := db.Table("background_jobs").Where("job_type = ? AND status = 'completed'", "integration_claim").Count(&completed).Error; err != nil {
		t.Fatalf("count completed: %v", err)
	}
	if completed != jobCount {
		t.Fatalf("completed jobs = %d, want %d", completed, jobCount)
	}
	stats, err := first.QueueStats(ctx)
	if err != nil {
		t.Fatalf("QueueStats(): %v", err)
	}
	if stats.Completed != jobCount || stats.Pending != 0 || stats.Running != 0 || stats.Dead != 0 {
		t.Fatalf("QueueStats() = %+v, want completed=%d and no unfinished jobs", stats, jobCount)
	}
}
