
.PHONY: run build test fmt vet db-up db-down db-logs migrate seed clean

run:
	go run ./cmd/server

build:
	go build -o bin/server ./cmd/server

test:
	go test ./...

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

clean:
	rm -rf bin
