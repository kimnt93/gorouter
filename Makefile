.PHONY: run build test tidy compose-postgres compose-clickhouse

run: build
	DATABASE_URL="$${DATABASE_URL:-postgres://gorouter:change-me-postgres-password@127.0.0.1:54329/gorouter}" \
	REDIS_URL="$${REDIS_URL:-redis://127.0.0.1:63899/0}" \
	MASTER_KEY="$${MASTER_KEY:-dev-master-key}" \
	ENCRYPTION_KEY="$${ENCRYPTION_KEY:-dev-encryption-key}" \
	./bin/gorouter

build:
	go build -o bin/gorouter ./cmd/gorouter
	go build -o bin/mock-gorouter ./cmd/mock-gorouter

test:
	TEST_DATABASE_URL="$${TEST_DATABASE_URL:-postgres://gorouter:change-me-postgres-password@127.0.0.1:54329/gorouter}" \
	go test ./...

tidy:
	go mod tidy

compose-postgres:
	docker compose --env-file .env -f docker-compose.postgres.yml up -d --build

compose-clickhouse:
	docker compose --env-file .env -f docker-compose.clickhouse.yml up -d --build
