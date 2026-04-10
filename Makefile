.PHONY: all build build-all run start-all dev test test-cover e2e lint mocks deps clean \
       docker-up docker-down docker-build migrate reset-db \
       security prometheus

# ---------------------------------------------------------------
# Variables
# ---------------------------------------------------------------
BINARY       := bin/gateway
SERVICES     := gateway auth-service wallet-service matching-engine \
                payment-service casino-service odds-service fraud-service \
                reporting-service risk-service hierarchy-service notification-service \
                admin-service
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

build-all:
	@mkdir -p bin
	@for svc in $(SERVICES); do \
		if [ -d "cmd/$$svc" ]; then \
			echo "Building $$svc..."; \
			CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/$$svc ./cmd/$$svc; \
		else \
			echo "Skipping $$svc (cmd/$$svc not found)"; \
		fi; \
	done
	@echo "All services built."

run: build
	./$(BINARY)

start-all: build-all
	./scripts/start-all.sh

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

# End-to-end tests (requires running gateway on :8080)
e2e:
	go run ./scripts/api-test

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
	@for migration in migrations/*.sql; do \
		if [ -f "$$migration" ]; then \
			echo "  → $$(basename $$migration)"; \
			psql "$(DATABASE_URL)" -f "$$migration" || exit 1; \
		fi; \
	done
	@echo "Migrations complete."

reset-db:
	@echo "Dropping and recreating the database..."
	$(COMPOSE) exec postgres psql -U lotus -c "DROP DATABASE IF EXISTS bettingdb;"
	$(COMPOSE) exec postgres psql -U lotus -c "CREATE DATABASE bettingdb;"
	@$(MAKE) migrate
	@echo "Database reset complete."

# ---------------------------------------------------------------
# Monitoring
# ---------------------------------------------------------------
prometheus:
	$(COMPOSE) up -d prometheus grafana
	@echo "Prometheus: http://localhost:$${PROMETHEUS_PORT:-9090}"
	@echo "Grafana:    http://localhost:$${GRAFANA_PORT:-3001}"

# ---------------------------------------------------------------
# Clean
# ---------------------------------------------------------------
clean:
	rm -rf bin/ coverage.out coverage.html
