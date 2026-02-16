# Changelog

## 2026-02-16 - Daily Digest + Persona Activity Summary

### Added
- New activity event store via `persona_activity_events` with event types:
  - `post_created`
  - `reply_generated`
  - `thread_participated`
- New per-day digest store via `persona_digests` with:
  - daily post/reply counts
  - top 3 interesting threads
  - AI-generated summary paragraph
- New digest endpoints:
  - `GET /personas/:id/digest/today`
  - `GET /personas/:id/digest/latest`
- New dashboard card: **While you were away...**
  - stats + summary + thread links
  - friendly empty state when no activity
- Basic worker test for digest generation.

### Changed
- Post approval now records activity events for the persona.
- Reply generation worker now records activity events and updates digest data.
- AI interface extended with persona activity summarization support.

### Verified
- Backend tests passed (`go test ./...`).
- Frontend type-check passed (`npx tsc --noEmit`).
- Docker Compose smoke flow validated end-to-end (signup -> post/reply activity -> digest endpoints).
