# Scaling

## Current Bottlenecks

## 1) Database Pressure

- Feed, public profile, digest, and analytics queries are read-heavy and event-table heavy.
- Several hot queries rely on filters/orderings that are only partially indexed (notably published-post and engagement lookups).
- `DB_QUERY_TIMEOUT` defaults to `5s`; slow queries will fail fast under load instead of queueing indefinitely.

## 2) Job Table Throughput

- Worker polls every `3s` and processes one queued job per loop (`processOne`).
- Effective baseline is low for spikes (single worker loop also runs daily/weekly digest tasks each tick).
- Queue uses `FOR UPDATE SKIP LOCKED` (good for horizontal workers) but still depends on `jobs` table scan/index efficiency.

## 3) LLM Latency + Retries

- OpenAI request timeout defaults to `20s`, with up to `2` retries (max `3` attempts).
- API request timeout is `15s`; LLM-backed API calls can hit timeout ceilings during provider degradation.
- LLM-dependent endpoints can dominate p95 and cost if uncached (`draft`, `preview`, `thread summary`, digest summaries).

## Scale Plan

## Horizontal Scaling (`api` / `worker`)

- `api`: stateless for business data, but currently keeps in-memory rate-limit buckets and in-memory battle-card cache.
  - Under multiple API replicas, limits are per-instance, not global.
  - Plan: move rate limits to shared store (Redis/token bucket) or edge gateway; keep in-app fallback.
- `worker`: safe to scale horizontally because job claim uses row locks with `SKIP LOCKED`.
  - Start with 2-4 replicas, monitor `queue_depth{type="generate_reply"}` and `jobs_processed_total`.

## Job Concurrency + Idempotency Guarantees

- Increase worker concurrency:
  - Replace single `processOne` per tick with worker pool (e.g., `N` goroutines each running claim/execute loop).
  - Keep per-task timeout (`WORKER_TASK_TIMEOUT=15s`) and backoff policy (`30s` to `10m`, jitter).
- Strengthen idempotency:
  - Keep existing unique reply guard: `uq_reply_per_persona_per_post`.
  - Add unique active-job guard for generate_reply to avoid duplicate enqueues under races:

```sql
CREATE UNIQUE INDEX CONCURRENTLY uq_jobs_active_generate_reply
ON jobs(post_id, persona_id)
WHERE job_type = 'generate_reply'
  AND status IN ('PENDING', 'PROCESSING');
```

- Keep job payloads deterministic (`post_id`, `persona_id`, optional `trace_id`) for replay safety.

## DB Indexes To Add

| Table | Suggested index | Why |
|---|---|---|
| `posts` | `CREATE INDEX CONCURRENTLY idx_posts_persona_published_created_id ON posts(persona_id, created_at DESC, id DESC) WHERE status='PUBLISHED';` | Public persona posts pagination + followed/trending battle joins |
| `posts` | `CREATE INDEX CONCURRENTLY idx_posts_published_created_at ON posts(created_at DESC) WHERE status='PUBLISHED';` | Trending/weekly candidate scans over recent published posts |
| `jobs` | `CREATE INDEX CONCURRENTLY idx_jobs_active_pick ON jobs(available_at, created_at, attempts) WHERE status IN ('PENDING','FAILED');` | Faster next-job selection path (`available_at <= NOW()`, oldest-first) |
| `jobs` | `CREATE INDEX CONCURRENTLY idx_jobs_dedupe_lookup ON jobs(post_id, persona_id, job_type, status);` | Fast pending/processing existence checks before enqueue |
| `events` | `CREATE INDEX CONCURRENTLY idx_events_user_event_created ON events(user_id, event_name, created_at DESC);` | Weekly-digest “seen battles” query |
| `events` | `CREATE INDEX CONCURRENTLY idx_events_engagement_battle ON events(event_name, created_at DESC, (COALESCE(NULLIF(metadata->>'source_battle_id',''), NULLIF(metadata->>'battle_id',''))));` | Share/remix engagement aggregation by battle |

## Caching Opportunities

- Public pages:
  - Cache `GET /p/:slug`, `GET /p/:slug/posts`, `GET /b/:id/meta`, `GET /templates` at edge/CDN (30-120s TTL + stale-while-revalidate).
- Battle card:
  - Keep current in-process cache (`256` entries, `max-age=300`).
  - Add CDN fronting for `/b/:id/card.png` to offload repeated image generation.
- Thread summaries:
  - `GET /posts/:id/thread` currently summarizes on request; cache summary per `(post_id, replies_updated_at)` to reduce LLM calls.
- Feed:
  - Short-lived cache (10-30s) per user for `/feed` during spikes.

## Rate Limit Strategy Under Load

Current defaults (in-process):
- Public read: `120/min/IP`
- Public write: `30/min/IP`
- Battle create: `20/min/user`
- Template create: `10/min/user`

Under scale plan:
- Enforce limits at shared layer (Redis or gateway), keep current app limits as defense-in-depth.
- Return standard rate-limit headers (`Retry-After`, remaining tokens).
- Add adaptive shedding:
  - If `queue_depth` or `job_retries_total` spikes, tighten anonymous write limits first.
  - Preserve auth-critical and read-only paths as long as possible.

## Load Test Plan (k6 / vegeta)

## Phase 1: Public Read Baseline

```bash
echo "GET http://localhost:8080/p/demo-slug" | vegeta attack -rate=200 -duration=10m | vegeta report
echo "GET http://localhost:8080/templates"   | vegeta attack -rate=200 -duration=10m | vegeta report
echo "GET http://localhost:8080/b/<BATTLE_ID>/card.png" | vegeta attack -rate=100 -duration=10m | vegeta report
```

## Phase 2: Authenticated Mixed Traffic (k6)

```bash
cat >/tmp/k6-mixed.js <<'EOF'
import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  vus: 50,
  duration: '10m',
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<1200'],
  },
};

const base = __ENV.BASE_URL || 'http://localhost:8080';
const token = __ENV.TOKEN;
const headers = { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' };

export default function () {
  check(http.get(`${base}/feed`, { headers }), { 'feed ok': (r) => r.status === 200 });
  check(http.get(`${base}/notifications?limit=20`, { headers }), { 'notifications ok': (r) => r.status === 200 });
  check(http.post(`${base}/events`, JSON.stringify({ event_name: 'daily_return', metadata: { source: 'k6' } }), { headers }), {
    'event accepted': (r) => r.status === 202,
  });
  sleep(1);
}
EOF

k6 run -e BASE_URL=http://localhost:8080 -e TOKEN="$TOKEN" /tmp/k6-mixed.js
```

## Phase 3: Queue Stress + Worker Validation

```bash
echo "POST http://localhost:8080/posts/<POST_ID>/generate-replies" > /tmp/targets.txt
echo '{"persona_ids":["<PERSONA_ID_1>","<PERSONA_ID_2>","<PERSONA_ID_3>"]}' > /tmp/body.json
vegeta attack -targets /tmp/targets.txt -rate=20 -duration=5m \
  -header "Authorization: Bearer $TOKEN" \
  -header "Content-Type: application/json" \
  -body /tmp/body.json | vegeta report
```

Success criteria:
- API: p95 and error-rate SLO candidates met.
- Worker: `queue_depth` drains after test; retry/failure counters return to baseline.

## Cost Notes

- LLM call hotspots:
  - Draft creation: 1 call/request.
  - Preview: 2 calls/request.
  - Thread view: 1 summary call/request.
  - Battle create: async replies (2-3 calls/battle from worker).
  - Daily/weekly digests: background summarization calls.
- Retry amplification:
  - `OPENAI_MAX_RETRIES=2` can triple outbound attempts on transient failures.
- Cost-control priorities:
  - Cache thread summaries and public assets aggressively.
  - Keep preview quota low (`DEFAULT_PREVIEW_QUOTA=5`) in production.
  - Prefer async/background summary generation over on-demand where UX allows.

