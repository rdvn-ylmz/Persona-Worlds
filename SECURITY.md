# Security and Hardening

## Threat Model

Primary risks for this service:

1. Abuse of public endpoints (scraping, brute-force, spam)
2. Data leakage from public profile/battle APIs
3. Long-running requests and resource exhaustion
4. Worker retry storms and duplicate side effects
5. Misconfigured CORS in production

## Mitigations Implemented

## Request and Transport Layer

- Global API request context timeout (`API_REQUEST_TIMEOUT`)
- Server socket timeouts (`API_READ_TIMEOUT`, `API_WRITE_TIMEOUT`, `API_IDLE_TIMEOUT`)
- Global request body cap (`REQUEST_BODY_MAX_BYTES`)
- Stricter body cap for public write paths (`PUBLIC_BODY_MAX_BYTES`)
- Security headers on all API responses:
  - `X-Content-Type-Options: nosniff`
  - `X-Frame-Options: DENY`
  - `Referrer-Policy: no-referrer`
  - `Content-Security-Policy` (API-safe default)
  - `Permissions-Policy` (minimal)

## Auth and Session Safety

- Bearer token auth enforced on protected routes
- Auth middleware returns consistent JSON error payloads
- Authorization tokens are never logged in structured logs
- Remix cookie is `HttpOnly`, `SameSite=Lax`, and `Secure` in production (`SECURE_COOKIES=true`)
- Remix cookie is invalidated after successful battle creation from remix token

## Input Validation

- Central UUID validation for route/query IDs
- Central topic bounds validation
- Slug normalization and regex validation for public profile slugs
- Pagination bounds validation for bounded list endpoints

## Rate Limiting

- Per-IP rate limits on public read/write routes
- Per-user creation rate limits for:
  - battle creation
  - template creation
- Rate-limit rejections are observable in logs and metrics

## Data Safety

- Public DTO mappers are used for public profile/public battle APIs
- Integration tests assert calibration fields do not leak from public APIs

## Worker and Retry Safety

- Job retries use exponential backoff with jitter (`JOB_RETRY_BASE`, `JOB_RETRY_MAX`)
- Maximum attempts are capped (`JOB_MAX_ATTEMPTS`)
- Job errors are truncated before persistence
- Reply generation jobs are idempotent for duplicate-reply cases
- Digest writes use UPSERT semantics to avoid duplicate rows

## Database Safety

- Database statement timeout is configured from `DB_QUERY_TIMEOUT`
- API and worker operations use cancellable contexts with deadlines

## Operational Guidance

- Keep `APP_ENV=prod` and set explicit `CORS_ALLOWED_ORIGINS` in production
- Rotate `JWT_SECRET` periodically and on incident
- Keep dependencies and base images updated
- Restrict database network access to service network only
