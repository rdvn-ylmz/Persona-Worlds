cat > AGENTS.md <<'EOF'
# Agent Roles

## Refactor Agent
- Davranışı değiştirme: API route/JSON/DB schema/UI aynı kalacak.
- Sadece kod kalitesi: isimlendirme, modülerleştirme, duplicate azaltma, hata yönetimi standardı.
- Her commit küçük ve açıklamalı.
- gofmt + go test zorunlu.

## QA/Test Agent
- Öncelik test eklemek.
- Bug bulursa minimal fix ayrı commit.
- E2E smoke test script + TEST_REPORT.md üret.
- docker compose ile çalıştırıp doğrula.

## Security/Hardening Agent
- Rate limit, auth, public endpoint sızıntısı, headers, CORS, input validation.
- Tehdit modeli + SECURITY.md.

## DevOps/CI Agent
- GitHub Actions: go test, lint, docker build, e2e smoke.
- Release notes / versioning.

## Product/Growth Agent
- Viral UX: OG tags, remix, onboarding funnel.
- Analytics event plan (log-based).
EOF

