# SLO

## SLIs

| SLI | Definition | Metric / Query |
|---|---|---|
| Availability | Share of non-5xx API responses | `1 - (sum(increase(http_requests_total{status=~"5..",route!~"/healthz|/metrics"}[30d])) / sum(increase(http_requests_total{route!~"/healthz|/metrics"}[30d])))` |
| p95 latency | 95th percentile API latency | `histogram_quantile(0.95, sum by (le) (rate(http_request_duration_seconds_bucket{route!~"/healthz|/metrics"}[5m])))` |
| Error rate | 5xx share over rolling window | `sum(rate(http_requests_total{status=~"5..",route!~"/healthz|/metrics"}[5m])) / sum(rate(http_requests_total{route!~"/healthz|/metrics"}[5m]))` |
| Job success rate | Completed job share (done vs done+failed) | `sum(rate(jobs_processed_total{status="done"}[15m])) / (sum(rate(jobs_processed_total{status=~"done|failed"}[15m])) + 1e-9)` |

Supporting indicators (not core SLI but required for diagnosis):
- `queue_depth{type}`
- `job_retries_total{type}`
- `db_query_duration_seconds`

## Proposed SLOs (Realistic)

## 1) API Availability

- Target: `99.9%` monthly (excluding `/healthz` and `/metrics` traffic).
- Error budget: ~`43m 49s` unavailable time per 30-day month.

## 2) p95 Latency

- Non-LLM routes (`/feed`, `/notifications`, `/templates`, public profile reads): p95 `< 750ms` for `99%` of 5-minute windows.
- LLM-coupled routes (`/rooms/{id}/posts/draft`, `/personas/{id}/preview`, `/posts/{id}/thread`): p95 `< 8s` for `99%` of 5-minute windows.

## 3) Error Rate

- Monthly target: 5xx ratio `< 1%`.
- Fast-burn guardrail: 5xx ratio `< 2%` over any 5-minute window.

## 4) Job Success Rate

- Rolling 24h target: success rate `>= 98.5%`.
- Fast-burn guardrail: success rate `>= 95%` over 15 minutes.

## Alert Recommendations (Based on Existing Metrics)

| Severity | Trigger | Example expression |
|---|---|---|
| P1 | Availability burn (fast) | `sum(rate(http_requests_total{status=~"5..",route!~"/healthz|/metrics"}[5m])) / sum(rate(http_requests_total{route!~"/healthz|/metrics"}[5m])) > 0.05` for 10m |
| P2 | Error rate elevated | same ratio `> 0.02` for 15m |
| P2 | Latency regression (non-LLM) | `histogram_quantile(0.95, sum by (le) (rate(http_request_duration_seconds_bucket{route!~"/rooms/\\{id\\}/posts/draft|/personas/\\{id\\}/preview|/posts/\\{id\\}/thread|/healthz|/metrics"}[10m]))) > 1.2` |
| P2 | LLM route latency spike | p95 for LLM routes `> 10s` for 15m |
| P1 | Worker success collapse | `sum(rate(jobs_processed_total{status="done"}[15m])) / (sum(rate(jobs_processed_total{status=~"done|failed"}[15m])) + 1e-9) < 0.90` |
| P2 | Retry storm | `increase(job_retries_total[10m]) > 30` |
| P2 | Queue backlog | `queue_depth{type="generate_reply"} > 200` for 15m |
| P2 | DB query latency high | `histogram_quantile(0.95, sum by (le) (rate(db_query_duration_seconds_bucket[10m]))) > 0.25` |

Operational notes:
- Scrape interval should be <= `15s` for responsive alerts.
- Treat `queue_depth` + `job_retries_total` correlation as worker saturation signal.
- Tune thresholds after first two weeks of production baseline data.
