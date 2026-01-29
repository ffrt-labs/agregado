include .env
export

.PHONY: dev-deps dev-deps-down dev build migrate-up migrate-down

dev-deps:
	docker-compose up -d

dev-deps-down:
	docker-compose down

dev:
	go run ./cmd/agregado

build:
	go build -o bin/agregado ./cmd/agregado

migrate-up:
	migrate -database ${POSTGRESQL_URL} -path migrations up

migrate-down:
	migrate -database ${POSTGRESQL_URL} -path migrations down
