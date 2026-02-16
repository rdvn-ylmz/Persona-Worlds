# Observability

This project exposes production-oriented observability for both `api` and `worker`.

## Structured Logs

API and worker logs are JSON lines and use a shared field model:

- `ts`
- `level`
- `msg`
- `service` (`api` or `worker`)
- `request_id` (for HTTP requests and job trace propagation)
- `route`
- `method`
- `status`
- `latency_ms`
- `user_id` (when available)
- `job_id` (worker job logs)

### View logs

With Docker Compose:

```bash
docker compose logs -f backend worker
```

Filter examples:

```bash
docker compose logs backend | jq 'select(.level=="error")'
docker compose logs worker | jq 'select(.job_id != null)'
```

## Health and Readiness

### API

- `GET /healthz`: process liveness (always 200 when process is alive)
- `GET /readyz`: readiness check (DB ping + migrations applied)

### Worker

Worker exposes a tiny HTTP observability server:

- `GET /healthz`
- `GET /metrics`

Default worker observability port is `9091` (`WORKER_OBSERVABILITY_PORT`).

## Metrics

### API metrics endpoint

- `GET /metrics`

### Core metrics

- `http_requests_total{route,method,status}`
- `http_request_duration_seconds_bucket{route,method}`
- `jobs_processed_total{type,status}`
- `job_duration_seconds_bucket{type}`
- `job_retries_total{type}`
- `db_query_duration_seconds_bucket`
- `queue_depth{type}`

Tracing-lite support also exposes:

- `jobs_trace_total{type,status,trace}` where `trace` is `present|missing`

## Tracing-lite

`request_id` is propagated into enqueued jobs as `trace_id` in job payload.

When worker processes jobs:

- `trace_id` is included in logs
- `request_id` is set to propagated trace where available
- metrics include low-cardinality trace state (`present|missing`)

## Prometheus + Grafana (Optional Compose Profile)

Compose includes optional observability services under profile `observability`.

Run:

```bash
docker compose --profile observability up -d
```

Access:

- Prometheus: `http://localhost:9090`
- Grafana: `http://localhost:3001` (`admin` / `admin`)

Provisioned dashboard:

- `PersonaWorlds Observability`

## Suggested Dashboards

1. API
- request rate by status
- p95 latency from `http_request_duration_seconds_bucket`
- 5xx error ratio

2. Worker
- job processed rate by status
- retry and failure trends
- queue depth by job type

## Suggested Alerts

1. High API latency
- p95 latency above threshold (e.g. > 500ms for 10m)

2. Error rate spike
- 5xx ratio above threshold (e.g. > 2% for 5m)

3. Worker failures
- `rate(jobs_processed_total{status="failed"}[10m])` above baseline

4. Retry storm
- `rate(job_retries_total[10m])` sustained increase

5. Queue backlog
- `queue_depth` increasing continuously for a job type
