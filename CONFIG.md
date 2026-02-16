# Configuration

## Required in Production

- `APP_ENV=prod`
- `DATABASE_URL`
- `JWT_SECRET`
- `FRONTEND_ORIGIN`
- `CORS_ALLOWED_ORIGINS` (comma-separated strict allowlist)
- `OPENAI_API_KEY` (if `LLM_PROVIDER=openai`)
- `SECURE_COOKIES=true`

## Backend Env Vars

- `APP_ENV` (default: `dev`)
- `PORT` (default: `8080`)
- `DATABASE_URL` (default local Postgres URL)
- `JWT_SECRET` (default: `change-me`)
- `LLM_PROVIDER` (default: `mock`)
- `OPENAI_BASE_URL` (default: `https://api.openai.com`)
- `OPENAI_API_KEY` (default: empty)
- `OPENAI_MODEL` (default: `gpt-4o-mini`)
- `OPENAI_REQUEST_TIMEOUT` (default: `20s`)
- `OPENAI_MAX_RETRIES` (default: `2`)
- `OPENAI_RETRY_BASE` (default: `400ms`)
- `MIGRATIONS_DIR` (default: `./migrations`)
- `FRONTEND_ORIGIN` (default: `http://localhost:3000`)
- `CORS_ALLOWED_ORIGINS` (default: auto from env + localhost in non-prod)
- `DRAFT_MAX_LEN` (default: `500`)
- `REPLY_MAX_LEN` (default: `280`)
- `SUMMARY_MAX_LEN` (default: `400`)
- `DEFAULT_DRAFT_QUOTA` (default: `5`)
- `DEFAULT_REPLY_QUOTA` (default: `25`)
- `DEFAULT_PREVIEW_QUOTA` (default: `5`)
- `REQUEST_BODY_MAX_BYTES` (default: `1048576`)
- `PUBLIC_BODY_MAX_BYTES` (default: `65536`)
- `API_REQUEST_TIMEOUT` (default: `15s`)
- `API_READ_TIMEOUT` (default: `15s`)
- `API_WRITE_TIMEOUT` (default: `30s`)
- `API_IDLE_TIMEOUT` (default: `60s`)
- `DB_QUERY_TIMEOUT` (default: `5s`)
- `WORKER_POLL_EVERY` (default: `3s`)
- `WORKER_TASK_TIMEOUT` (default: `15s`)
- `WORKER_OBSERVABILITY_PORT` (default: `9091`)
- `SECURE_COOKIES` (default: `false` in dev, `true` in prod)
- `JOB_MAX_ATTEMPTS` (default: `5`)
- `JOB_RETRY_BASE` (default: `30s`)
- `JOB_RETRY_MAX` (default: `10m`)

## Frontend Env Vars

- `NEXT_PUBLIC_API_BASE_URL` (default: `http://localhost:8080`)

## Notes

- `DB_QUERY_TIMEOUT` is applied as Postgres `statement_timeout` for all pooled connections.
- If `CORS_ALLOWED_ORIGINS` is not set:
  - production defaults to `FRONTEND_ORIGIN` only
  - non-production also allows localhost origins
