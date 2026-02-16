#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

API_BASE="${API_BASE:-http://127.0.0.1:8080}"
USE_HOSTNET="${USE_HOSTNET:-0}"

if ! command -v docker >/dev/null 2>&1; then
  echo "[smoke] FAIL: docker command not found" >&2
  exit 1
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "[smoke] FAIL: curl command not found" >&2
  exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "[smoke] FAIL: jq command not found" >&2
  exit 1
fi

compose_files=(-f docker-compose.yml)
if [[ "$USE_HOSTNET" == "1" ]]; then
  compose_files+=(-f docker-compose.hostnet.yml)
fi

compose() {
  docker compose "${compose_files[@]}" "$@"
}

log() {
  printf '[smoke] %s\n' "$*"
}

fail() {
  printf '[smoke] FAIL: %s\n' "$*" >&2
  exit 1
}

cleanup() {
  log "Stopping compose stack"
  compose down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

HTTP_STATUS=""
HTTP_BODY=""

request() {
  local method="$1"
  local url="$2"
  local token="${3:-}"
  local body="${4:-}"
  local response
  local -a curl_args

  curl_args=(-sS -X "$method" "$url" -H "Content-Type: application/json")
  if [[ -n "$token" ]]; then
    curl_args+=(-H "Authorization: Bearer $token")
  fi
  if [[ -n "$body" ]]; then
    curl_args+=(-d "$body")
  fi

  response="$(curl "${curl_args[@]}" -w $'\n%{http_code}')"
  HTTP_STATUS="${response##*$'\n'}"
  HTTP_BODY="${response%$'\n'*}"
}

assert_status() {
  local expected="$1"
  if [[ "$HTTP_STATUS" != "$expected" ]]; then
    fail "expected HTTP ${expected}, got ${HTTP_STATUS}, body: ${HTTP_BODY}"
  fi
}

assert_no_calibration_fields() {
  local payload="$1"
  if jq -e '.. | objects | keys[] | select(.=="writing_samples" or .=="do_not_say" or .=="catchphrases")' >/dev/null <<<"$payload"; then
    fail "response leaked calibration fields: ${payload}"
  fi
}

wait_for_backend() {
  local max_attempts=60
  local attempt=1

  while [[ "$attempt" -le "$max_attempts" ]]; do
    if curl -fsS "${API_BASE}/healthz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
    attempt=$((attempt + 1))
  done

  fail "backend health check did not become ready"
}

log "Starting compose services"
compose up --build -d postgres backend worker

log "Running seed"
compose --profile tools run --rm seed >/dev/null

log "Waiting for backend health"
wait_for_backend

EMAIL="smoke-$(date +%s)-$RANDOM@example.com"
PASSWORD="password123"

log "Signup"
request POST "${API_BASE}/auth/signup" "" "{\"email\":\"${EMAIL}\",\"password\":\"${PASSWORD}\"}"
assert_status 201
TOKEN="$(jq -r '.token // empty' <<<"$HTTP_BODY")"
[[ -n "$TOKEN" ]] || fail "missing token in signup response"

log "Load rooms"
request GET "${API_BASE}/rooms" "$TOKEN" ""
assert_status 200
ROOM_ID="$(jq -r '.rooms[0].id // empty' <<<"$HTTP_BODY")"
[[ -n "$ROOM_ID" ]] || fail "missing room id"

PERSONA_PAYLOAD="$(jq -nc '{
  name: "Smoke Persona",
  bio: "Validates API safety and quota behavior.",
  tone: "direct",
  writing_samples: ["Ship small", "Measure outcomes", "Ask one sharp question"],
  do_not_say: ["guaranteed growth"],
  catchphrases: ["ship, learn, iterate"],
  preferred_language: "en",
  formality: 1,
  daily_draft_quota: 1,
  daily_reply_quota: 25
}')"

log "Create persona"
request POST "${API_BASE}/personas" "$TOKEN" "$PERSONA_PAYLOAD"
assert_status 201
PERSONA_ID="$(jq -r '.id // empty' <<<"$HTTP_BODY")"
[[ -n "$PERSONA_ID" ]] || fail "missing persona id"

PREVIEW_URL="${API_BASE}/personas/${PERSONA_ID}/preview?room_id=${ROOM_ID}"
log "Consume preview quota"
for i in 1 2 3 4 5; do
  request POST "$PREVIEW_URL" "$TOKEN" "{}"
  assert_status 200
done

log "Preview quota should be exhausted"
request POST "$PREVIEW_URL" "$TOKEN" "{}"
assert_status 429
if ! jq -e '.error == "daily preview quota reached"' >/dev/null <<<"$HTTP_BODY"; then
  fail "unexpected preview limit response: ${HTTP_BODY}"
fi

DRAFT_BODY="$(jq -nc --arg persona_id "$PERSONA_ID" '{persona_id: $persona_id}')"
log "Create first battle draft"
request POST "${API_BASE}/rooms/${ROOM_ID}/posts/draft" "$TOKEN" "$DRAFT_BODY"
assert_status 201
POST_ID="$(jq -r '.id // empty' <<<"$HTTP_BODY")"
[[ -n "$POST_ID" ]] || fail "missing post id"

log "Second battle draft should hit daily limit"
request POST "${API_BASE}/rooms/${ROOM_ID}/posts/draft" "$TOKEN" "$DRAFT_BODY"
assert_status 429
if ! jq -e '.error == "daily draft quota reached"' >/dev/null <<<"$HTTP_BODY"; then
  fail "unexpected draft limit response: ${HTTP_BODY}"
fi

log "Approve post for battle/thread endpoint"
request POST "${API_BASE}/posts/${POST_ID}/approve" "$TOKEN" "{}"
assert_status 200

log "Publish public profile"
request POST "${API_BASE}/personas/${PERSONA_ID}/publish-profile" "$TOKEN" "{}"
assert_status 200
SLUG="$(jq -r '.slug // empty' <<<"$HTTP_BODY")"
[[ -n "$SLUG" ]] || fail "missing slug"

log "Check public profile does not leak calibration fields"
request GET "${API_BASE}/p/${SLUG}" "" ""
assert_status 200
assert_no_calibration_fields "$HTTP_BODY"

log "Check battle endpoint does not leak calibration fields"
request GET "${API_BASE}/b/${POST_ID}" "$TOKEN" ""
assert_status 200
assert_no_calibration_fields "$HTTP_BODY"

log "Smoke test passed"
