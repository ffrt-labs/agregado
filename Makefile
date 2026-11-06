include .env
export

.PHONY: dev-deps dev-deps-down dev build migrate-up migrate-down migrate-ownership migrate-ownership-apply legacy-dump seed-langfuse-prompts experiment experiment-test

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

# Idempotent: creates each Langfuse-managed prompt's v1 only if absent (#201).
seed-langfuse-prompts:
	go run ./cmd/seed-langfuse-prompts

# Langfuse experiment (#206): run a prompt label against a dataset, e.g.
#   make experiment LABEL=staging ITEMS=5 [DATASET=score-golden] [OPERATION=preference_score]
# ITEMS is required: every item spends Workers AI neurons. Run after the
# 08:00 Digest. See evals/README.md. LANGFUSE_EVAL_BASE_URL overrides the
# docker-network LANGFUSE_BASE_URL for runs from outside that network.
experiment:
	cd evals && LANGFUSE_BASE_URL="$(or $(LANGFUSE_EVAL_BASE_URL),$(LANGFUSE_BASE_URL))" \
		uv run python -m agregado_evals \
		--operation "$(or $(OPERATION),preference_score)" \
		--dataset "$(or $(DATASET),score-golden)" \
		--label "$(LABEL)" --max-items="$(ITEMS)"

# The harness's evaluator unit tests; no network.
experiment-test:
	cd evals && uv run pytest -q

# Final cold dump of the old Postgres, with a restore check. Run last.
legacy-dump:
	scripts/legacy-postgres-dump.sh ./backups
