# Runbook

## Scope

Services:
- API: `:8080` (`/healthz`, `/readyz`, `/metrics`)
- Worker observability: `:9091` (`/healthz`, `/metrics`)
- DB: Postgres (`personaworlds`)

Key defaults that affect incidents:
- `API_REQUEST_TIMEOUT=15s`
- `DB_QUERY_TIMEOUT=5s`
- `WORKER_POLL_EVERY=3s`
- `WORKER_TASK_TIMEOUT=15s`
- `JOB_MAX_ATTEMPTS=5`

## First 10 Minutes (Triage)

1. Confirm blast radius:
   - Is it public endpoints only, auth-only, or all API traffic?
   - Is queue processing delayed or failing?
2. Check liveness/readiness quickly:
   - `curl -sS http://localhost:8080/healthz`
   - `curl -sS http://localhost:8080/readyz`
   - `curl -sS http://localhost:9091/healthz`
3. Pull high-signal metrics:
   - `curl -sS http://localhost:8080/metrics | rg "http_requests_total|http_request_duration_seconds|db_query_duration_seconds|queue_depth|rate_limit_events_total"`
   - `curl -sS http://localhost:9091/metrics | rg "jobs_processed_total|job_retries_total|job_duration_seconds|db_query_duration_seconds"`
4. Inspect structured logs:
   - `docker compose logs --since=15m backend worker`
   - `docker compose logs backend | jq 'select(.level=="error")'`
   - `docker compose logs worker | jq 'select(.level=="error" or .msg=="job_failed_retrying" or .msg=="job_failed_permanently")'`

## Debugging With Logs + Metrics

## API-side signals

- High latency:
  - Metric: `http_request_duration_seconds`
  - Log field: `latency_ms`, `route`, `status`
- Error spikes:
  - Metric: `http_requests_total{status=~"5.."}`
  - Log field: `msg`, `error`, `route`
- DB contention:
  - Metric: `db_query_duration_seconds`
  - Log clues: readiness failures, slow request routes
- Rate limiting:
  - Metric: `rate_limit_events_total{scope,endpoint}`
  - Log message: `rate_limited`

## Worker-side signals

- Backlog:
  - Metric: `queue_depth{type}`
- Retry storm:
  - Metric: `job_retries_total{type}`
  - Log message: `job_failed_retrying`
- Permanent failures:
  - Metric: `jobs_processed_total{status="failed"}`
  - Log message: `job_failed_permanently`

## Common Failure Modes + Remediation

## 1) `/readyz` returns `503`

Symptoms:
- `database ping failed` or `migrations pending` in readiness output/logs.

Checks:
- `docker compose logs backend | rg "database ping failed|migrations pending|startup_failed"`
- `psql "$DATABASE_URL" -c "select count(*) from schema_migrations;"`
- Compare with SQL files in `backend/migrations`.

Remediation:
1. Restore DB connectivity (network/credentials/DB health).
2. Apply missing migrations.
3. Restart API and verify `/readyz` becomes `200`.

## 2) API p95 latency spike / request timeouts

Symptoms:
- Rising `http_request_duration_seconds` p95.
- User-facing timeouts around `15s`.

Checks:
- Route-level latency in metrics.
- `db_query_duration_seconds` p95.
- Logs with `latency_ms > 1000`.

Remediation:
1. Identify top slow routes first (often feed/public/event-heavy queries).
2. Reduce pressure: temporarily lower non-essential traffic or tighten public rate limits.
3. Apply missing indexes from `docs/SCALING.md`.
4. If LLM-backed route is dominant, switch to degraded mode (`LLM_PROVIDER=mock`) until provider stabilizes.

## 3) 5xx error rate spike

Symptoms:
- `http_requests_total{status=~"5.."}` surge.

Checks:
- Error logs grouped by `route` and `msg`.
- Correlate with deploy/config changes.

Remediation:
1. Roll back last risky change if immediately correlated.
2. Fix config regressions (`DATABASE_URL`, `JWT_SECRET`, CORS/env mismatches).
3. Confirm recovery by watching 5xx ratio and `/readyz`.

## 4) Worker backlog / delayed replies

Symptoms:
- `queue_depth{type="generate_reply"}` climbs continuously.
- `jobs_processed_total{status="done"}` flat.

Checks:
- Worker logs for repeated timeout/provider errors.
- Retry counters (`job_retries_total`).

Remediation:
1. Scale worker replicas horizontally.
2. Verify DB performance for job claim query.
3. If LLM is degraded, reduce enqueue rate and/or switch temporary provider mode.
4. After stabilization, confirm queue drains.

## 5) Retry storm in worker

Symptoms:
- Rapid growth in `job_retries_total`.
- Frequent `job_failed_retrying` logs.

Checks:
- Error payload in logs (provider errors vs validation/permanent issues).

Remediation:
1. If provider/network issue: reduce pressure, retry later, or degrade provider.
2. If data/validation issue: patch bug causing deterministic failures.
3. Consider temporarily raising backoff or pausing worker while patch deploys.

## 6) Public endpoint abuse / rate-limit spikes

Symptoms:
- High `rate_limit_events_total` on `public_read` / `public_write`.

Checks:
- Logs for repeated offending client IP patterns.

Remediation:
1. Block abusive IPs at edge/WAF.
2. Tighten public limits temporarily.
3. Move rate-limit enforcement to shared layer (Redis/edge) if running multiple API replicas.

## 7) Digest/notification freshness complaints

Symptoms:
- Users do not see digest refreshes or expected notifications.

Checks:
- Worker done/failed metrics.
- DB rows in `persona_digests`, `weekly_digests`, `notifications`.
- Trigger-path logs around follow/remix/template usage.

Remediation:
1. Ensure worker is healthy and processing tasks.
2. Re-run missing generation path after root cause fix.
3. Validate queue and event ingestion are healthy.

## Recovery Verification Checklist

1. `/readyz` and worker `/healthz` stable for 15+ minutes.
2. 5xx ratio returned to baseline.
3. p95 latency under target.
4. `queue_depth` no longer increasing and retries normalized.
5. Incident timeline + root cause + preventive action documented.

