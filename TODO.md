# TODO - PersonaWorlds Next Steps

## Progress Summary
- Persona Calibration completed end-to-end:
  - Backend stores `writing_samples`, `do_not_say`, `catchphrases`, `preferred_language`, `formality`.
  - New endpoint: `POST /personas/:id/preview?room_id=...` returns 2 drafts.
  - Preview quota implemented as separate daily quota (`preview`, default 5/day).
  - Prompting updated to include calibration signals and enforce short/non-spam style with `1 insight + 1 question`.
- Frontend completed:
  - Persona form now supports calibration fields.
  - `Preview Voice` flow implemented.
  - Preview results shown with `AI Preview` labels.
  - Persona edit/save and re-run preview supported.
- Testing and runtime checks completed:
  - Integration test added for preview endpoint.
  - Docker compose startup verified with host-network override.
  - Seed + API smoke flow verified.

## Next Steps

### P0 - Stabilize and Ship
- [ ] Commit and push current calibration/preview changes.
- [ ] Add CI workflow:
  - [ ] `go test ./...`
  - [ ] frontend build check
  - [ ] optional docker-compose smoke stage
- [ ] Add API contract examples for preview endpoint response/error cases in README.
- [ ] Add explicit validation error messages in frontend form for:
  - [ ] `writing_samples` must be exactly 3 lines
  - [ ] language/formality constraints

### P1 - Product Quality
- [ ] Add preview quota visibility in backend list/get persona responses (used/limit for today).
- [ ] Add retry UX on preview failures and inline error state near preview panel.
- [ ] Support explicit room style presets in preview UI (e.g., technical, debate, tutorial).
- [ ] Add minimum profanity/link-spam unit tests for preview outputs.

### P2 - Reliability and Ops
- [ ] Add structured request logging fields for preview endpoint (`persona_id`, `room_id`, quota state).
- [ ] Add rate limiting per user on preview endpoint.
- [ ] Add migration rollback notes and backup guidance in README.
- [ ] Add health/readiness checks for worker queue lag.

### P3 - Future Enhancements
- [ ] Persona calibration import/export JSON.
- [ ] Multi-language preview expansion beyond `tr/en`.
- [ ] A/B preview mode with side-by-side voting.
- [ ] Persist preview history for persona tuning analytics.
