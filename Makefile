include .env
export

.PHONY: dev-deps dev-deps-down dev build migrate-up migrate-down migrate-ownership migrate-ownership-apply legacy-dump

dev-deps:
	docker-compose up -d

dev-deps-down:
	docker-compose down -v

dev:
	go run ./cmd/agregado

build:
	go build -o bin/agregado ./cmd/agregado

migrate-up:
	migrate -database ${POSTGRESQL_URL} -path migrations up

migrate-down:
	migrate -database ${POSTGRESQL_URL} -path migrations down

# Ownership-based migration (issue #83). Dry run by default; see
# docs/runbooks/legacy-postgres-cutover.md.
migrate-ownership:
	go run ./cmd/migrate-ownership

migrate-ownership-apply:
	go run ./cmd/migrate-ownership -apply

# Final cold dump of the old Postgres, with a restore check. Run last.
legacy-dump:
	scripts/legacy-postgres-dump.sh ./backups
