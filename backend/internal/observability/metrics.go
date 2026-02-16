package observability

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var defaultDurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type histogram struct {
	buckets []float64
	counts  []uint64
	count   uint64
	sum     float64
}

func newHistogram(buckets []float64) *histogram {
	copyBuckets := make([]float64, len(buckets))
	copy(copyBuckets, buckets)
	return &histogram{
		buckets: copyBuckets,
		counts:  make([]uint64, len(copyBuckets)),
	}
}

func (h *histogram) observe(value float64) {
	if h == nil {
		return
	}
	if value < 0 {
		value = 0
	}
	for idx, bucket := range h.buckets {
		if value <= bucket {
			h.counts[idx]++
			break
		}
	}
	h.count++
	h.sum += value
}

type apiRequestKey struct {
	route  string
	method string
	status string
}

type apiDurationKey struct {
	route  string
	method string
}

type APIMetrics struct {
	mu            sync.RWMutex
	httpRequests  map[apiRequestKey]uint64
	httpDurations map[apiDurationKey]*histogram
	dbQuery       *histogram
	queueDepth    map[string]float64
	rateLimited   map[rateLimitKey]uint64
}

func NewAPIMetrics() *APIMetrics {
	return &APIMetrics{
		httpRequests:  map[apiRequestKey]uint64{},
		httpDurations: map[apiDurationKey]*histogram{},
		dbQuery:       newHistogram(defaultDurationBuckets),
		queueDepth:    map[string]float64{},
		rateLimited:   map[rateLimitKey]uint64{},
	}
}

type rateLimitKey struct {
	scope    string
	endpoint string
}

func (m *APIMetrics) ObserveHTTPRequest(route, method string, status int, duration time.Duration) {
	if m == nil {
		return
	}
	key := apiRequestKey{
		route:  normalizeMetricValue(route, "unknown"),
		method: normalizeMetricValue(strings.ToUpper(strings.TrimSpace(method)), "UNKNOWN"),
		status: normalizeMetricValue(strconv.Itoa(status), "0"),
	}
	durationKey := apiDurationKey{route: key.route, method: key.method}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.httpRequests[key]++
	h, exists := m.httpDurations[durationKey]
	if !exists {
		h = newHistogram(defaultDurationBuckets)
		m.httpDurations[durationKey] = h
	}
	h.observe(duration.Seconds())
}

func (m *APIMetrics) ObserveDBQuery(duration time.Duration) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dbQuery.observe(duration.Seconds())
}

func (m *APIMetrics) SetQueueDepthSnapshot(values map[string]int) {
	if m == nil {
		return
	}
	snapshot := map[string]float64{}
	for jobType, count := range values {
		cleanType := normalizeMetricValue(jobType, "unknown")
		if count < 0 {
			count = 0
		}
		snapshot[cleanType] = float64(count)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.queueDepth = snapshot
}

func (m *APIMetrics) IncRateLimited(scope, endpoint string) {
	if m == nil {
		return
	}
	key := rateLimitKey{
		scope:    normalizeMetricValue(scope, "unknown"),
		endpoint: normalizeMetricValue(endpoint, "unknown"),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rateLimited[key]++
}

func (m *APIMetrics) Render() string {
	if m == nil {
		return ""
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var sb strings.Builder

	sb.WriteString("# HELP http_requests_total Total HTTP requests handled by API.\n")
	sb.WriteString("# TYPE http_requests_total counter\n")
	httpRequestKeys := make([]apiRequestKey, 0, len(m.httpRequests))
	for key := range m.httpRequests {
		httpRequestKeys = append(httpRequestKeys, key)
	}
	sort.Slice(httpRequestKeys, func(i, j int) bool {
		if httpRequestKeys[i].route != httpRequestKeys[j].route {
			return httpRequestKeys[i].route < httpRequestKeys[j].route
		}
		if httpRequestKeys[i].method != httpRequestKeys[j].method {
			return httpRequestKeys[i].method < httpRequestKeys[j].method
		}
		return httpRequestKeys[i].status < httpRequestKeys[j].status
	})
	for _, key := range httpRequestKeys {
		labels := map[string]string{
			"route":  key.route,
			"method": key.method,
			"status": key.status,
		}
		sb.WriteString("http_requests_total")
		sb.WriteString(formatLabels(labels))
		sb.WriteString(" ")
		sb.WriteString(strconv.FormatUint(m.httpRequests[key], 10))
		sb.WriteString("\n")
	}

	sb.WriteString("# HELP http_request_duration_seconds HTTP request latency in seconds.\n")
	sb.WriteString("# TYPE http_request_duration_seconds histogram\n")
	httpDurationKeys := make([]apiDurationKey, 0, len(m.httpDurations))
	for key := range m.httpDurations {
		httpDurationKeys = append(httpDurationKeys, key)
	}
	sort.Slice(httpDurationKeys, func(i, j int) bool {
		if httpDurationKeys[i].route != httpDurationKeys[j].route {
			return httpDurationKeys[i].route < httpDurationKeys[j].route
		}
		return httpDurationKeys[i].method < httpDurationKeys[j].method
	})
	for _, key := range httpDurationKeys {
		labels := map[string]string{
			"route":  key.route,
			"method": key.method,
		}
		renderHistogramSeries(&sb, "http_request_duration_seconds", labels, m.httpDurations[key])
	}

	sb.WriteString("# HELP db_query_duration_seconds Database query duration in seconds.\n")
	sb.WriteString("# TYPE db_query_duration_seconds histogram\n")
	renderHistogramSeries(&sb, "db_query_duration_seconds", map[string]string{}, m.dbQuery)

	sb.WriteString("# HELP queue_depth Pending or in-flight jobs by type.\n")
	sb.WriteString("# TYPE queue_depth gauge\n")
	queueTypes := make([]string, 0, len(m.queueDepth))
	for jobType := range m.queueDepth {
		queueTypes = append(queueTypes, jobType)
	}
	sort.Strings(queueTypes)
	for _, jobType := range queueTypes {
		labels := map[string]string{"type": jobType}
		sb.WriteString("queue_depth")
		sb.WriteString(formatLabels(labels))
		sb.WriteString(" ")
		sb.WriteString(strconv.FormatFloat(m.queueDepth[jobType], 'g', -1, 64))
		sb.WriteString("\n")
	}

	sb.WriteString("# HELP rate_limit_events_total Rate-limit rejections by scope and endpoint.\n")
	sb.WriteString("# TYPE rate_limit_events_total counter\n")
	limitedKeys := make([]rateLimitKey, 0, len(m.rateLimited))
	for key := range m.rateLimited {
		limitedKeys = append(limitedKeys, key)
	}
	sort.Slice(limitedKeys, func(i, j int) bool {
		if limitedKeys[i].scope != limitedKeys[j].scope {
			return limitedKeys[i].scope < limitedKeys[j].scope
		}
		return limitedKeys[i].endpoint < limitedKeys[j].endpoint
	})
	for _, key := range limitedKeys {
		labels := map[string]string{
			"scope":    key.scope,
			"endpoint": key.endpoint,
		}
		sb.WriteString("rate_limit_events_total")
		sb.WriteString(formatLabels(labels))
		sb.WriteString(" ")
		sb.WriteString(strconv.FormatUint(m.rateLimited[key], 10))
		sb.WriteString("\n")
	}

	return sb.String()
}

type workerProcessedKey struct {
	jobType string
	status  string
}

type workerTraceKey struct {
	jobType string
	status  string
	trace   string
}

type WorkerMetrics struct {
	mu            sync.RWMutex
	jobsProcessed map[workerProcessedKey]uint64
	jobDurations  map[string]*histogram
	jobRetries    map[string]uint64
	jobsTrace     map[workerTraceKey]uint64
	dbQuery       *histogram
}

func NewWorkerMetrics() *WorkerMetrics {
	return &WorkerMetrics{
		jobsProcessed: map[workerProcessedKey]uint64{},
		jobDurations:  map[string]*histogram{},
		jobRetries:    map[string]uint64{},
		jobsTrace:     map[workerTraceKey]uint64{},
		dbQuery:       newHistogram(defaultDurationBuckets),
	}
}

func (m *WorkerMetrics) ObserveJobProcessed(jobType, status, traceID string, duration time.Duration) {
	if m == nil {
		return
	}
	cleanType := normalizeMetricValue(jobType, "unknown")
	cleanStatus := normalizeMetricValue(status, "unknown")
	traceState := "missing"
	if strings.TrimSpace(traceID) != "" {
		traceState = "present"
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobsProcessed[workerProcessedKey{jobType: cleanType, status: cleanStatus}]++
	h, exists := m.jobDurations[cleanType]
	if !exists {
		h = newHistogram(defaultDurationBuckets)
		m.jobDurations[cleanType] = h
	}
	h.observe(duration.Seconds())
	m.jobsTrace[workerTraceKey{jobType: cleanType, status: cleanStatus, trace: traceState}]++
}

func (m *WorkerMetrics) IncrementJobRetry(jobType string) {
	if m == nil {
		return
	}
	cleanType := normalizeMetricValue(jobType, "unknown")
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobRetries[cleanType]++
}

func (m *WorkerMetrics) ObserveDBQuery(duration time.Duration) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dbQuery.observe(duration.Seconds())
}

func (m *WorkerMetrics) Render() string {
	if m == nil {
		return ""
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var sb strings.Builder

	sb.WriteString("# HELP jobs_processed_total Total processed jobs by type and status.\n")
	sb.WriteString("# TYPE jobs_processed_total counter\n")
	processedKeys := make([]workerProcessedKey, 0, len(m.jobsProcessed))
	for key := range m.jobsProcessed {
		processedKeys = append(processedKeys, key)
	}
	sort.Slice(processedKeys, func(i, j int) bool {
		if processedKeys[i].jobType != processedKeys[j].jobType {
			return processedKeys[i].jobType < processedKeys[j].jobType
		}
		return processedKeys[i].status < processedKeys[j].status
	})
	for _, key := range processedKeys {
		labels := map[string]string{"type": key.jobType, "status": key.status}
		sb.WriteString("jobs_processed_total")
		sb.WriteString(formatLabels(labels))
		sb.WriteString(" ")
		sb.WriteString(strconv.FormatUint(m.jobsProcessed[key], 10))
		sb.WriteString("\n")
	}

	sb.WriteString("# HELP job_duration_seconds Job processing latency in seconds.\n")
	sb.WriteString("# TYPE job_duration_seconds histogram\n")
	jobTypes := make([]string, 0, len(m.jobDurations))
	for jobType := range m.jobDurations {
		jobTypes = append(jobTypes, jobType)
	}
	sort.Strings(jobTypes)
	for _, jobType := range jobTypes {
		labels := map[string]string{"type": jobType}
		renderHistogramSeries(&sb, "job_duration_seconds", labels, m.jobDurations[jobType])
	}

	sb.WriteString("# HELP job_retries_total Total retries scheduled by job type.\n")
	sb.WriteString("# TYPE job_retries_total counter\n")
	retryTypes := make([]string, 0, len(m.jobRetries))
	for jobType := range m.jobRetries {
		retryTypes = append(retryTypes, jobType)
	}
	sort.Strings(retryTypes)
	for _, jobType := range retryTypes {
		labels := map[string]string{"type": jobType}
		sb.WriteString("job_retries_total")
		sb.WriteString(formatLabels(labels))
		sb.WriteString(" ")
		sb.WriteString(strconv.FormatUint(m.jobRetries[jobType], 10))
		sb.WriteString("\n")
	}

	sb.WriteString("# HELP jobs_trace_total Processed jobs grouped by trace availability.\n")
	sb.WriteString("# TYPE jobs_trace_total counter\n")
	traceKeys := make([]workerTraceKey, 0, len(m.jobsTrace))
	for key := range m.jobsTrace {
		traceKeys = append(traceKeys, key)
	}
	sort.Slice(traceKeys, func(i, j int) bool {
		if traceKeys[i].jobType != traceKeys[j].jobType {
			return traceKeys[i].jobType < traceKeys[j].jobType
		}
		if traceKeys[i].status != traceKeys[j].status {
			return traceKeys[i].status < traceKeys[j].status
		}
		return traceKeys[i].trace < traceKeys[j].trace
	})
	for _, key := range traceKeys {
		labels := map[string]string{
			"type":   key.jobType,
			"status": key.status,
			"trace":  key.trace,
		}
		sb.WriteString("jobs_trace_total")
		sb.WriteString(formatLabels(labels))
		sb.WriteString(" ")
		sb.WriteString(strconv.FormatUint(m.jobsTrace[key], 10))
		sb.WriteString("\n")
	}

	sb.WriteString("# HELP db_query_duration_seconds Database query duration in seconds.\n")
	sb.WriteString("# TYPE db_query_duration_seconds histogram\n")
	renderHistogramSeries(&sb, "db_query_duration_seconds", map[string]string{}, m.dbQuery)

	return sb.String()
}

func renderHistogramSeries(sb *strings.Builder, metricName string, labels map[string]string, h *histogram) {
	if sb == nil || h == nil {
		return
	}

	cumulative := uint64(0)
	for idx, bucket := range h.buckets {
		cumulative += h.counts[idx]
		withLE := cloneLabels(labels)
		withLE["le"] = strconv.FormatFloat(bucket, 'g', -1, 64)
		sb.WriteString(metricName)
		sb.WriteString("_bucket")
		sb.WriteString(formatLabels(withLE))
		sb.WriteString(" ")
		sb.WriteString(strconv.FormatUint(cumulative, 10))
		sb.WriteString("\n")
	}

	withInf := cloneLabels(labels)
	withInf["le"] = "+Inf"
	sb.WriteString(metricName)
	sb.WriteString("_bucket")
	sb.WriteString(formatLabels(withInf))
	sb.WriteString(" ")
	sb.WriteString(strconv.FormatUint(h.count, 10))
	sb.WriteString("\n")

	sb.WriteString(metricName)
	sb.WriteString("_sum")
	sb.WriteString(formatLabels(labels))
	sb.WriteString(" ")
	sb.WriteString(strconv.FormatFloat(h.sum, 'g', -1, 64))
	sb.WriteString("\n")

	sb.WriteString(metricName)
	sb.WriteString("_count")
	sb.WriteString(formatLabels(labels))
	sb.WriteString(" ")
	sb.WriteString(strconv.FormatUint(h.count, 10))
	sb.WriteString("\n")
}

func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+`="`+escapeLabelValue(labels[key])+`"`)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func cloneLabels(labels map[string]string) map[string]string {
	out := make(map[string]string, len(labels))
	for key, value := range labels {
		out[key] = value
	}
	return out
}

func escapeLabelValue(value string) string {
	replacer := strings.NewReplacer(`\\`, `\\\\`, `\n`, `\\n`, `"`, `\\"`)
	return replacer.Replace(value)
}

func normalizeMetricValue(value, fallback string) string {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return fallback
	}
	return clean
}
