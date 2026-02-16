# TEST_REPORT

Date: 2026-02-16
Role: QA/Test Agent

## Scope
- Added integration coverage for calibration data leakage on public and battle reads.
- Added integration coverage for preview quota and battle daily limit behavior.
- Added Docker-based E2E smoke script.

## Added Integration Tests
- `TestPublicProfileDoesNotLeakPersonaCalibrationFieldsIntegration`
- `TestBattleEndpointDoesNotLeakPersonaCalibrationFieldsIntegration`
- `TestPreviewQuotaAndBattleDailyLimitIntegration`

Location: `backend/internal/api/privacy_quota_integration_test.go`

## How To Run

### 1) Backend test suite
```bash
cd backend
GOCACHE=/tmp/go-build go test ./...
```

### 2) New integration tests (requires Postgres)
```bash
docker compose -f docker-compose.yml -f docker-compose.hostnet.yml up -d postgres
cd backend
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/personaworlds?sslmode=disable' \
  GOCACHE=/tmp/go-build \
  go test ./internal/api -run 'TestPublicProfileDoesNotLeakPersonaCalibrationFieldsIntegration|TestBattleEndpointDoesNotLeakPersonaCalibrationFieldsIntegration|TestPreviewQuotaAndBattleDailyLimitIntegration' -v
docker compose -f docker-compose.yml -f docker-compose.hostnet.yml down -v --remove-orphans
```

### 3) E2E smoke flow
```bash
./scripts/smoke.sh
```

If Docker bridge networking is unsupported on your host:
```bash
USE_HOSTNET=1 ./scripts/smoke.sh
```

## Results In This Environment
- `GOCACHE=/tmp/go-build go test ./...` -> PASS
- New integration tests (3 scenarios above) -> PASS
- `./scripts/smoke.sh` (bridge mode) -> FAIL in this kernel (`veth ... operation not supported`)
- `USE_HOSTNET=1 ./scripts/smoke.sh` -> PASS

## Notes
- Minimal compatibility fix was applied to expose battle read alias: `GET /b/{id}` now maps to the existing thread handler.
