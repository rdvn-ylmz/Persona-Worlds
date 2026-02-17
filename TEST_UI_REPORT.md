# TEST_UI_REPORT

## Scope
- Role: Frontend QA / E2E Test Agent
- Date: 2026-02-17
- Environment: `docker compose` (postgres + backend + worker + frontend) + seed data
- Browser automation: Playwright with system Chromium (`/usr/bin/chromium`)
- Test scripts:
  - `frontend/e2e/ui-smoke.cjs`
  - `frontend/e2e/ui-offline-check.cjs`

## Test Users
- Owner: `ui-audit-owner-1771319954827-5946@example.com`
- Follower/Remixer: `ui-audit-follower-1771319954827-7138@example.com`
- Extra feed verification users were created for `Open battle` feed CTA coverage.

## Issues (NO-OP BUTTON)
No silent-fail / no-op button was reproduced in tested flows.

## Fully Working Flows
- Home/Auth
  - Sign up from home form works and enters authenticated dashboard.
- Profile/Persona
  - Create Persona works (API + state update + feedback).
  - Publish profile works (API + slug + feedback).
  - Unpublish works with feedback.
- Rooms/Posts
  - Create AI Draft works.
  - Approve & Publish works.
  - Load Thread works.
  - View Battle Card opens `/b/:id`.
- Feed
  - Refresh Feed works.
  - Empty topic validation shows explicit error.
  - Use template works (state updates).
  - Create Battle works.
  - Async polling progresses to DONE and UI updates.
  - Feed `Open battle` CTA opens battle page.
- Battle Page (`/b/:id`)
  - Remix this battle opens modal.
  - Create remixed battle sends API and starts async flow.
  - Cancel closes remix modal.
  - Copy link works with feedback.
  - Copy image works with feedback.
  - Share shows explicit success/fallback feedback.
  - Dashboard link navigation works.
- Public Battle (`/b/:id`, unauthenticated)
  - Remix redirects to signup with remix flow context.
- Templates
  - Refresh works.
  - Create Template works.
  - Use template redirects back to feed compose flow.
- Public Persona (`/p/:slug`)
  - Follow works for authenticated non-owner.
  - Owner self-follow is blocked with explicit feedback.
  - Navigation CTA back to dashboard works.
- Notifications
  - Refresh works.
  - Notification item click marks read and navigates to target.
  - Mark all read clears unread badge.
- Weekly Digest
  - Refresh works.
  - Open battle works when item exists.
- Top Navigation
  - Feed/Rooms/Profile hash navigation works.
  - Templates link navigation works.

## Backend Disconnect Error Handling
Backend was temporarily stopped during UI session and actions were re-tested:
- Refresh Feed (backend offline): explicit error feedback shown.
- Create AI Draft (backend offline): explicit error feedback shown.
- Notifications Refresh (backend offline): explicit error feedback shown.

Result: no silent failure observed under backend outage for tested actions.

## Partially Broken Flows
- None reproduced.

## Severity Summary
- High: 0
- Medium: 0
- Low: 0
