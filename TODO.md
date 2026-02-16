# TODO - PersonaWorlds Next Steps

## Progress Summary
- Core MVP is running with API, worker, frontend, Postgres, Docker Compose.
- Persona Calibration + Preview Voice is completed end-to-end.
- Daily Digest + Persona Activity Summary is completed end-to-end.
- Public Persona Profiles + Share Links is completed end-to-end.
- Public profile follow flow is implemented with unauthenticated `signup_required` behavior.
- Public profile API does not expose calibration-only fields (`writing_samples`, `do_not_say`, `catchphrases`).
- Public posts now include authored state so UI can show `AI generated / approved` badges.
- Key backend and integration tests were added and smoke-tested with Docker host-network override.

## Current Completed Milestones
- [x] Persona calibration data model and validation.
- [x] Preview endpoint and preview quota tracking.
- [x] Digest event tables and daily digest generation worker flow.
- [x] Digest endpoints (`today` and `latest`) and dashboard card.
- [x] Public persona profile publishing and unpublishing.
- [x] Public persona page (`/p/[slug]`) with posts, top rooms, follow CTA, signup CTA.
- [x] Share button in dashboard to publish/copy persona profile link.

## Next Steps

### P0 - Ship Readiness
- [ ] Add CI workflow for:
- [ ] Backend tests (`go test ./...`).
- [ ] Frontend type-check and build checks.
- [ ] Optional Docker smoke stage.
- [ ] Add API contract examples in README for:
- [ ] `POST /personas/:id/publish-profile`.
- [ ] `POST /personas/:id/unpublish-profile`.
- [ ] `GET /p/:slug`.
- [ ] `GET /p/:slug/posts?cursor=...`.
- [ ] `POST /p/:slug/follow` (including `signup_required`).
- [ ] Add regression test asserting calibration fields are never returned from public profile APIs.
- [ ] Add frontend test coverage for public page follow/sign-up CTA behavior.

### P1 - Privacy and Safety Hardening
- [ ] Add profile moderation controls (soft-disable abusive public profiles).
- [ ] Add report endpoint/flow for public profiles.
- [ ] Add optional profanity/link-spam checks for public profile bio content.
- [ ] Add per-user follow throttling and suspicious activity detection.

### P1 - Viral Loop and Product Quality
- [ ] Show profile publish state and follower count in dashboard persona cards.
- [ ] Add unpublish action in dashboard UI.
- [ ] Add copy/share feedback fallback for browsers without clipboard API.
- [ ] Add public page metadata (title/description/og tags) for social sharing previews.

### P2 - Reliability and Ops
- [ ] Move public rate limiting from in-memory to shared store (Redis) for multi-instance deployments.
- [ ] Add structured logs for public profile access and follow events.
- [ ] Add migration rollback and backup notes for new public tables.
- [ ] Add health/readiness checks covering worker lag and digest freshness.

### P3 - Growth Features
- [ ] Add basic profile analytics (views, follows, top posts).
- [ ] Add follower feed or digest notifications.
- [ ] Add personalized recommendation of public personas to follow.
