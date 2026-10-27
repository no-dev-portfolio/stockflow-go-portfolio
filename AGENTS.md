# StockFlow maintenance
- Read README.md, docs/DESIGN.md and docs/REQUIREMENTS_MATRIX.md before changes.
- Preserve tenant isolation, role authorization, sorted stock locks, all-or-nothing writes, idempotency payload binding, expiry checks and atomic outbox/audit writes.
- Use real PostgreSQL integration tests. scripts/check.sh must reject a missing TEST_DATABASE_URL.
- Run gofmt, go vet, go test -race, frontend type checks/tests and dependency audits for affected changes.
- Do not commit runtime credentials, .env, DB dumps, Terraform state, caches, or node_modules.
- Local demo only. Do not claim cloud deployment, third-party review, production operation or professional Go experience without evidence.
