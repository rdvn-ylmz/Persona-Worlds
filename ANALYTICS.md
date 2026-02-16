# Analytics Plan

This project uses first-party, database-backed analytics only.
No external analytics provider is used.

## Funnel

Primary funnel (7-day trend):

1. `share` -> `battle_shared`
2. `view` -> `public_profile_viewed`
3. `signup` -> `signup_from_share`
4. `persona` -> `persona_created`
5. `battle` -> `battle_created`

Remix funnel metric:

- `remix_completed / public_views`
- `public_views` maps to `public_battle_viewed`

Retention funnel extension:

- `daily_return`
- `notification_clicked`
- `template_used_from_feed`

## Tracked Events

Core product events:

- `persona_created`
- `preview_generated`
- `post_approved`
- `battle_created`
- `battle_shared`
- `public_profile_viewed`
- `public_battle_viewed`
- `signup_from_share`
- `remix_clicked`
- `remix_started`
- `remix_completed`

Frontend interaction signals (via `POST /events`):

- `remix_click` (legacy)
- `remix_clicked`
- `follow_click`
- `daily_return`
- `notification_clicked`
- `template_used_from_feed`

## Data Model

`events` table:

- `id` (bigserial primary key)
- `user_id` (nullable uuid)
- `event_name` (text)
- `metadata` (jsonb)
- `created_at` (timestamptz)

## API

- `POST /events`
  - Body: `{ "event_name": "<name>", "metadata": { ... } }`
  - Lightweight ingestion endpoint for client-side events.
- `GET /admin/analytics/summary` (JWT required)
  - Returns per-event counts for last `24h` and `7d`.
  - Returns `funnel_7d` snapshot (`share -> view -> signup -> persona -> battle`).

## Privacy Rules

- Event metadata is sanitized server-side.
- Keys that may include raw user text are dropped (`content`, `message`, `text`, `body`).
- String metadata values are truncated.
- No raw draft/reply/message text is stored in analytics metadata.
