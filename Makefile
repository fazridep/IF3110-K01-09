.PHONY: run worker seed-large build test test-db-up fmt vet db-up db-down db-logs migrate seed clean frontend-install frontend-dev frontend-build frontend-test

export TEST_DATABASE_URL ?= postgres://test:test@localhost:5437/platform_test?sslmode=disable

run:
	go run ./cmd/server

worker:
	go run ./cmd/worker

build:
	go build -o bin/server ./cmd/server
	go build -o bin/worker ./cmd/worker

test:
	go test -count=1 ./...

test-db-up:
	docker compose up -d db-test

fmt:
	gofmt -w .

vet:
	go vet ./...

db-up:
	docker compose up -d db db-target

db-down:
	docker compose down

db-logs:
	docker compose logs -f db

migrate:
	go run ./cmd/server migrate

seed:
	go run ./cmd/server seed-admin

seed-large:
	docker compose exec -T db-target psql -U target -d target_db -v rows=$${ROWS:-2000000} < scripts/seed_large_table.sql

clean:
	rm -rf bin frontend/dist

frontend-install:
	cd frontend && npm ci

frontend-dev:
	cd frontend && npm run dev

frontend-build:
	cd frontend && npm run build

frontend-test:
	cd frontend && npm test
