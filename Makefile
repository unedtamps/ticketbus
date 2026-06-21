.PHONY: help infra-up infra-down infra-logs infra-clean \
        dev dev-auth dev-event dev-inventory dev-payment dev-web \
        build build-auth build-event build-inventory build-payment clean \
        migrate migrate-auth-up migrate-event-up migrate-inventory-up migrate-payment-up \
        test lint format direnv-allow

# Default target
help:
	@echo "TicketSaas development commands:"
	@echo "  make infra-up              Start Docker infrastructure"
	@echo "  make infra-down            Stop Docker infrastructure"
	@echo "  make infra-logs            Tail Docker logs"
	@echo "  make infra-clean           Stop and remove volumes"
	@echo ""
	@echo "  make dev-auth              Run auth service"
	@echo "  make dev-event             Run event service"
	@echo "  make dev-inventory         Run inventory service"
	@echo "  make dev-payment           Run payment service"
	@echo "  make dev-web               Run Next.js frontend"
	@echo ""
	@echo "  make build                 Build all binaries to bin/"
	@echo "  make clean                 Remove bin/ directory"
	@echo ""
	@echo "  make direnv-allow          Allow all .envrc files"
	@echo ""
	@echo "  make migrate               Run all migrations"
	@echo "  make test                  Run all tests"
	@echo "  make lint                  Run Go linter"
	@echo "  make format                Format Go code"

# Infrastructure
infra-up:
	docker compose -f docker/docker-compose.yml up -d

infra-down:
	docker compose -f docker/docker-compose.yml down

infra-logs:
	docker compose -f docker/docker-compose.yml logs -f

infra-clean:
	docker compose -f docker/docker-compose.yml down -v

dev:
	@echo "starting all services..."
	@$(MAKE) dev-auth & \
	$(MAKE) dev-event & \
	$(MAKE) dev-inventory & \
	$(MAKE) dev-payment & \
	$(MAKE) dev-web & \
	wait

# Services
dev-auth:
	direnv exec cmd/auth-service  go run ./cmd/auth-service

dev-event:
	direnv exec cmd/event-service go run ./cmd/event-service

dev-inventory:
	direnv exec cmd/inventory-service go run ./cmd/inventory-service

dev-payment:
	direnv exec cmd/payment-service go run ./cmd/payment-service

dev-web:
	npx turbo run dev

direnv-allow:
	direnv allow cmd/auth-service
	direnv allow cmd/event-service
	direnv allow cmd/inventory-service
	direnv allow cmd/payment-service

# Builds
build: build-auth build-event build-inventory build-payment

build-auth:
	@mkdir -p bin
	go build -o bin/auth-service ./cmd/auth-service

build-event:
	@mkdir -p bin
	go build -o bin/event-service ./cmd/event-service

build-inventory:
	@mkdir -p bin
	go build -o bin/inventory-service ./cmd/inventory-service

build-payment:
	@mkdir -p bin
	go build -o bin/payment-service ./cmd/payment-service

clean:
	rm -rf bin

# Migrations (override via env var, e.g. DATABASE_URL_AUTH=... make migrate-auth-up)
DATABASE_URL_AUTH ?= postgres://ticketsaas:ticketsaas@localhost:5432/auth_db?sslmode=disable
DATABASE_URL_EVENT ?= postgres://ticketsaas:ticketsaas@localhost:5433/event_db?sslmode=disable
DATABASE_URL_INVENTORY ?= postgres://ticketsaas:ticketsaas@localhost:5434/inventory_db?sslmode=disable
DATABASE_URL_PAYMENT ?= postgres://ticketsaas:ticketsaas@localhost:5435/payment_db?sslmode=disable

migrate-auth-up:
	migrate -path migrations/auth -database "$(DATABASE_URL_AUTH)" up

migrate-event-up:
	migrate -path migrations/event -database "$(DATABASE_URL_EVENT)" up

migrate-inventory-up:
	migrate -path migrations/inventory -database "$(DATABASE_URL_INVENTORY)" up

migrate-payment-up:
	migrate -path migrations/payment -database "$(DATABASE_URL_PAYMENT)" up

migrate: migrate-auth-up migrate-event-up migrate-inventory-up migrate-payment-up

# Testing
test:
	go test ./...

# Linting
lint:
	golangci-lint run ./...

format:
	gofmt -w .
