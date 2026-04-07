.PHONY: all build run dev test test-cover e2e lint mocks deps clean \
       docker-up docker-down docker-build migrate reset-db seed \
       docs security loadtest prometheus

# ---------------------------------------------------------------
# Variables
# ---------------------------------------------------------------
BINARY       := bin/gateway
COMPOSE      := docker compose -f deployments/docker-compose.yml
DATABASE_URL ?= $(shell grep DATABASE_URL .env 2>/dev/null | cut -d= -f2-)

# ---------------------------------------------------------------
# Full build pipeline
# ---------------------------------------------------------------
all: deps lint test build

# ---------------------------------------------------------------
# Build & Run
# ---------------------------------------------------------------
build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY) ./cmd/gateway

run: build
	./$(BINARY)

# Run with hot reload (requires air: go install github.com/air-verse/air@latest)
dev:
	air -c .air.toml

# ---------------------------------------------------------------
# Testing
# ---------------------------------------------------------------
test:
	go test ./... -v -race -count=1 -timeout 120s

test-cover:
	go test ./... -coverprofile=coverage.out -race -timeout 120s
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# End-to-end tests (requires running services)
e2e:
	go test ./tests/e2e/... -v -tags=e2e -count=1 -timeout 300s

# ---------------------------------------------------------------
# Code Quality
# ---------------------------------------------------------------
lint:
	golangci-lint run ./...

# Security audit with gosec
security:
	@command -v gosec >/dev/null 2>&1 || { echo "Installing gosec..."; go install github.com/securego/gosec/v2/cmd/gosec@latest; }
	gosec -quiet ./...

mocks:
	go generate ./...

# ---------------------------------------------------------------
# Dependencies
# ---------------------------------------------------------------
deps:
	go mod tidy
	go mod download
	go mod verify

# ---------------------------------------------------------------
# Docker
# ---------------------------------------------------------------
docker-up:
	$(COMPOSE) up -d

docker-down:
	$(COMPOSE) down

docker-build:
	$(COMPOSE) build

docker-logs:
	$(COMPOSE) logs -f

# ---------------------------------------------------------------
# Database
# ---------------------------------------------------------------
migrate:
	@echo "Running migrations against $(DATABASE_URL)"
	psql "$(DATABASE_URL)" -f migrations/001_initial.sql

reset-db:
	@echo "Dropping and recreating the database..."
	$(COMPOSE) exec postgres psql -U lotus -c "DROP DATABASE IF EXISTS bettingdb;"
	$(COMPOSE) exec postgres psql -U lotus -c "CREATE DATABASE bettingdb;"
	@$(MAKE) migrate
	@echo "Database reset complete."

seed:
	@echo "Seeding demo data..."
	psql "$(DATABASE_URL)" -f scripts/seed.sql
	@echo "Seed complete."

# ---------------------------------------------------------------
# Documentation
# ---------------------------------------------------------------
docs:
	@command -v swag >/dev/null 2>&1 || { echo "Installing swag..."; go install github.com/swaggo/swag/cmd/swag@latest; }
	swag init -g cmd/gateway/main.go -o docs/swagger --parseInternal
	@echo "API docs generated in docs/swagger/"

# ---------------------------------------------------------------
# Monitoring
# ---------------------------------------------------------------
prometheus:
	$(COMPOSE) up -d prometheus grafana
	@echo "Prometheus: http://localhost:$${PROMETHEUS_PORT:-9090}"
	@echo "Grafana:    http://localhost:$${GRAFANA_PORT:-3001}"

# ---------------------------------------------------------------
# Load Test
# ---------------------------------------------------------------
loadtest:
	k6 run scripts/loadtest.js

# ---------------------------------------------------------------
# Clean
# ---------------------------------------------------------------
clean:
	rm -rf bin/ coverage.out coverage.html docs/swagger/
