.PHONY: run build test tidy css compose-postgres compose-clickhouse

run: build
	./bin/gorouter

build:
	go build -o bin/gorouter ./cmd/gorouter
	go build -o bin/mock-gorouter ./cmd/mock-gorouter

test:
	TEST_DATABASE_URL="$${TEST_DATABASE_URL:-postgres://gorouter:change-me-postgres-password@127.0.0.1:54329/gorouter}" \
	TEST_REDIS_URL="$${TEST_REDIS_URL:-redis://127.0.0.1:63899/0}" \
	go test ./...

tidy:
	go mod tidy

css:
	npm run css

compose-postgres:
	docker compose --env-file .env -f docker-compose.postgres.yml up -d --build

compose-clickhouse:
	docker compose --env-file .env -f docker-compose.clickhouse.yml up -d --build
