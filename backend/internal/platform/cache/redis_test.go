package cache

import (
	"context"
	"testing"
	"time"

	"github.com/lsy/blog/internal/config"
)

func TestNewReturnsClientWhenInitialPingFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	client, err := New(ctx, config.RedisConfig{
		Addr:         "127.0.0.1:1",
		DialTimeout:  10 * time.Millisecond,
		ReadTimeout:  10 * time.Millisecond,
		WriteTimeout: 10 * time.Millisecond,
		PoolSize:     1,
	})
	if err == nil {
		t.Fatal("expected initial ping to fail")
	}
	if client == nil {
		t.Fatal("expected a retained client so Redis can recover without process restart")
	}
	if closeErr := client.Close(); closeErr != nil {
		t.Fatalf("close retained client: %v", closeErr)
	}
}
