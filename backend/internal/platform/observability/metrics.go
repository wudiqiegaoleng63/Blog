package observability

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lsy/blog/internal/platform/jobs"
)

// Metrics is a small dependency-free Prometheus exposition registry. It keeps
// only bounded operational labels; request paths use Gin route templates and
// provider metrics never contain prompts, article text, or credentials.
type Metrics struct {
	mu sync.RWMutex

	httpRequests   map[httpMetricKey]uint64
	httpDuration   map[httpDurationKey]durationMetric
	rateLimit      map[rateMetricKey]uint64
	upstream       map[upstreamMetricKey]uint64
	upstreamDur    map[upstreamDurationKey]durationMetric
	workerJobs     map[workerJobKey]uint64
	workerClaims   uint64
	workerReclaims uint64
	queue          map[string]float64
}

type httpMetricKey struct{ method, route, status string }
type httpDurationKey struct{ method, route string }
type rateMetricKey struct{ scope, event string }
type upstreamMetricKey struct{ provider, operation, status string }
type upstreamDurationKey struct{ provider, operation string }
type workerJobKey struct{ jobType, result string }
type durationMetric struct {
	count uint64
	sum   float64
}

func NewMetrics() *Metrics {
	return &Metrics{
		httpRequests: make(map[httpMetricKey]uint64),
		httpDuration: make(map[httpDurationKey]durationMetric),
		rateLimit:    make(map[rateMetricKey]uint64),
		upstream:     make(map[upstreamMetricKey]uint64),
		upstreamDur:  make(map[upstreamDurationKey]durationMetric),
		workerJobs:   make(map[workerJobKey]uint64),
		queue:        make(map[string]float64),
	}
}

func (m *Metrics) ObserveHTTP(method, route string, status int, duration time.Duration) {
	if m == nil {
		return
	}
	method = boundedLabel(method, "unknown")
	route = boundedLabel(route, "unmatched")
	statusText := strconv.Itoa(status)
	m.mu.Lock()
	m.httpRequests[httpMetricKey{method, route, statusText}]++
	key := httpDurationKey{method, route}
	m.httpDuration[key] = addDuration(m.httpDuration[key], duration)
	m.mu.Unlock()
}

func (m *Metrics) ObserveRateLimit(scope, event string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.rateLimit[rateMetricKey{boundedLabel(scope, "unknown"), boundedLabel(event, "unknown")}]++
	m.mu.Unlock()
}

func (m *Metrics) ObserveUpstream(provider, operation string, status int, duration time.Duration) {
	if m == nil {
		return
	}
	provider = boundedLabel(provider, "unknown")
	operation = boundedLabel(operation, "unknown")
	statusText := strconv.Itoa(status)
	m.mu.Lock()
	m.upstream[upstreamMetricKey{provider, operation, statusText}]++
	key := upstreamDurationKey{provider, operation}
	m.upstreamDur[key] = addDuration(m.upstreamDur[key], duration)
	m.mu.Unlock()
}

func (m *Metrics) ObserveWorkerJob(jobType, result string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.workerJobs[workerJobKey{boundedLabel(jobType, "unknown"), boundedLabel(result, "unknown")}]++
	m.mu.Unlock()
}

func (m *Metrics) ObserveWorkerClaimError() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.workerClaims++
	m.mu.Unlock()
}

func (m *Metrics) ObserveWorkerReclaims(count int64) {
	if m == nil || count <= 0 {
		return
	}
	m.mu.Lock()
	m.workerReclaims += uint64(count)
	m.mu.Unlock()
}

func (m *Metrics) SetQueueStats(stats jobs.QueueStats) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.queue["pending"] = float64(stats.Pending)
	m.queue["running"] = float64(stats.Running)
	m.queue["dead"] = float64(stats.Dead)
	m.queue["completed"] = float64(stats.Completed)
	m.queue["oldest_pending_age_seconds"] = stats.OldestPendingAge(time.Now().UTC()).Seconds()
	m.mu.Unlock()
}

func (m *Metrics) SetHeartbeatAge(age time.Duration) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.queue["heartbeat_age_seconds"] = age.Seconds()
	m.mu.Unlock()
}

// Handler returns a Prometheus-compatible HTTP handler for internal scraping.
func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(m.Render()))
	})
}

func (m *Metrics) Render() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var b strings.Builder
	writeHelpType(&b, "blog_http_requests_total", "Total HTTP requests.", "counter")
	for _, key := range sortedHTTPKeys(m.httpRequests) {
		fmt.Fprintf(&b, "blog_http_requests_total%s %d\n", labels(map[string]string{"method": key.method, "route": key.route, "status": key.status}), m.httpRequests[key])
	}
	writeHelpType(&b, "blog_http_request_duration_seconds", "HTTP request duration in seconds.", "summary")
	for _, key := range sortedHTTPDurationKeys(m.httpDuration) {
		metric := m.httpDuration[key]
		labelsText := labels(map[string]string{"method": key.method, "route": key.route})
		fmt.Fprintf(&b, "blog_http_request_duration_seconds_count%s %d\n", labelsText, metric.count)
		fmt.Fprintf(&b, "blog_http_request_duration_seconds_sum%s %.9f\n", labelsText, metric.sum)
	}
	writeHelpType(&b, "blog_rate_limit_events_total", "Rate limit enforcement events.", "counter")
	for _, key := range sortedRateKeys(m.rateLimit) {
		fmt.Fprintf(&b, "blog_rate_limit_events_total%s %d\n", labels(map[string]string{"scope": key.scope, "event": key.event}), m.rateLimit[key])
	}
	writeHelpType(&b, "blog_upstream_requests_total", "External provider requests.", "counter")
	for _, key := range sortedUpstreamKeys(m.upstream) {
		fmt.Fprintf(&b, "blog_upstream_requests_total%s %d\n", labels(map[string]string{"provider": key.provider, "operation": key.operation, "status": key.status}), m.upstream[key])
	}
	writeHelpType(&b, "blog_upstream_request_duration_seconds", "External provider request duration in seconds.", "summary")
	for _, key := range sortedUpstreamDurationKeys(m.upstreamDur) {
		metric := m.upstreamDur[key]
		labelsText := labels(map[string]string{"provider": key.provider, "operation": key.operation})
		fmt.Fprintf(&b, "blog_upstream_request_duration_seconds_count%s %d\n", labelsText, metric.count)
		fmt.Fprintf(&b, "blog_upstream_request_duration_seconds_sum%s %.9f\n", labelsText, metric.sum)
	}
	writeHelpType(&b, "blog_worker_jobs_total", "Background jobs processed by result.", "counter")
	for _, key := range sortedWorkerKeys(m.workerJobs) {
		fmt.Fprintf(&b, "blog_worker_jobs_total%s %d\n", labels(map[string]string{"job_type": key.jobType, "result": key.result}), m.workerJobs[key])
	}
	writeHelpType(&b, "blog_worker_claim_errors_total", "Worker job claim errors.", "counter")
	fmt.Fprintf(&b, "blog_worker_claim_errors_total %d\n", m.workerClaims)
	writeHelpType(&b, "blog_worker_stale_reclaims_total", "Stale worker locks reclaimed.", "counter")
	fmt.Fprintf(&b, "blog_worker_stale_reclaims_total %d\n", m.workerReclaims)
	writeHelpType(&b, "blog_worker_queue", "Worker queue state gauges.", "gauge")
	for _, state := range sortedQueueKeys(m.queue) {
		fmt.Fprintf(&b, "blog_worker_queue%s %.9f\n", labels(map[string]string{"state": state}), m.queue[state])
	}
	return b.String()
}

func addDuration(current durationMetric, duration time.Duration) durationMetric {
	current.count++
	current.sum += duration.Seconds()
	return current
}

func boundedLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return fallback
	}
	return value
}

func labels(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for index, key := range keys {
		if index > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `%s="%s"`, key, escapeLabel(values[key]))
	}
	b.WriteByte('}')
	return b.String()
}

func escapeLabel(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(value)
}

func writeHelpType(b *strings.Builder, name, help, metricType string) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, metricType)
}

func sortedHTTPKeys(values map[httpMetricKey]uint64) []httpMetricKey {
	keys := make([]httpMetricKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j]) })
	return keys
}
func sortedHTTPDurationKeys(values map[httpDurationKey]durationMetric) []httpDurationKey {
	keys := make([]httpDurationKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j]) })
	return keys
}
func sortedRateKeys(values map[rateMetricKey]uint64) []rateMetricKey {
	keys := make([]rateMetricKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j]) })
	return keys
}
func sortedUpstreamKeys(values map[upstreamMetricKey]uint64) []upstreamMetricKey {
	keys := make([]upstreamMetricKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j]) })
	return keys
}
func sortedUpstreamDurationKeys(values map[upstreamDurationKey]durationMetric) []upstreamDurationKey {
	keys := make([]upstreamDurationKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j]) })
	return keys
}
func sortedWorkerKeys(values map[workerJobKey]uint64) []workerJobKey {
	keys := make([]workerJobKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j]) })
	return keys
}
func sortedQueueKeys(values map[string]float64) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
