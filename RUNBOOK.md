# Runbook

## Services

- API: `:8080`
- Worker observability: `:9091`
- API health: `GET /healthz`
- API readiness: `GET /readyz`
- API metrics: `GET /metrics`
- Worker metrics: `GET http://localhost:9091/metrics`

## Common Incidents

## 1) API readiness failing (`/readyz` returns 503)

Check:

1. API logs for `database ping failed` or migration mismatch
2. Postgres health/status
3. `schema_migrations` count vs files in `backend/migrations`

Commands:

```bash
docker compose logs backend
psql "$DATABASE_URL" -c "select count(*) from schema_migrations;"
```

## 2) High API latency / timeouts

Check:

1. `http_request_duration_seconds` p95
2. `db_query_duration_seconds`
3. API logs with high `latency_ms`

Commands:

```bash
curl -s http://localhost:8080/metrics | rg "http_request_duration_seconds|db_query_duration_seconds"
docker compose logs backend | jq 'select(.latency_ms != null and .latency_ms > 1000)'
```

## 3) Rate limit spikes

Check:

1. `rate_limit_events_total`
2. API logs with `msg=rate_limited`

Commands:

```bash
curl -s http://localhost:8080/metrics | rg rate_limit_events_total
docker compose logs backend | jq 'select(.msg=="rate_limited")'
```

## 4) Worker backlog / retry storms

Check:

1. `queue_depth`
2. `jobs_processed_total{status="retry"|"failed"}`
3. `job_retries_total`

Commands:

```bash
curl -s http://localhost:8080/metrics | rg queue_depth
curl -s http://localhost:9091/metrics | rg "jobs_processed_total|job_retries_total"
docker compose logs worker | jq 'select(.job_id != null)'
```

## 5) OpenAI provider failures

Check:

1. API/worker logs containing provider errors
2. Retry behavior in logs
3. OpenAI env vars (`OPENAI_API_KEY`, timeout/retry settings)

## Recovery Steps

1. Verify DB health and connectivity first.
2. Confirm migrations are fully applied.
3. If retries are storming, temporarily reduce worker concurrency/poll speed or pause worker.
4. If external LLM is unstable, switch to `LLM_PROVIDER=mock` for degraded mode.
5. Redeploy with corrected env/config and monitor `/readyz` + metrics.
