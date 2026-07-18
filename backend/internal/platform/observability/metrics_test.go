package observability

import (
	"strings"
	"testing"
	"time"

	"github.com/lsy/blog/internal/platform/jobs"
)

func TestMetricsRenderUsesBoundedOperationalLabels(t *testing.T) {
	metrics := NewMetrics()
	metrics.ObserveHTTP("GET", "/posts/:slug", 200, 25*time.Millisecond)
	metrics.ObserveRateLimit("ai:ask", "rejected")
	metrics.ObserveUpstream("openai_compatible", "chat", 503, 100*time.Millisecond)
	metrics.ObserveWorkerJob("post_index", "failed")
	metrics.ObserveWorkerClaimError()
	metrics.ObserveWorkerReclaims(2)
	oldest := time.Now().UTC().Add(-time.Minute)
	metrics.SetQueueStats(jobs.QueueStats{Pending: 1, Dead: 2, OldestPendingAt: &oldest})

	output := metrics.Render()
	for _, want := range []string{
		`blog_http_requests_total{method="GET",route="/posts/:slug",status="200"} 1`,
		`blog_rate_limit_events_total{event="rejected",scope="ai:ask"} 1`,
		`blog_upstream_requests_total{operation="chat",provider="openai_compatible",status="503"} 1`,
		`blog_worker_jobs_total{job_type="post_index",result="failed"} 1`,
		`blog_worker_stale_reclaims_total 2`,
		`blog_worker_queue{state="pending"} 1.000000000`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("metrics output missing %q:\n%s", want, output)
		}
	}
	for _, forbidden := range []string{"password", "cookie", "authorization", "article content"} {
		if strings.Contains(strings.ToLower(output), forbidden) {
			t.Fatalf("metrics output contains sensitive text %q", forbidden)
		}
	}
}
