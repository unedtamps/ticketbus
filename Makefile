.PHONY: help infra-up infra-down infra-logs infra-clean \
        dev dev-auth dev-ticketing dev-payment dev-web \
        build build-auth build-ticketing build-payment clean \
        migrate migrate-auth-up migrate-ticketing-up migrate-payment-up \
        test lint format direnv-allow integration-test integration-test-race \
        e2e-keys e2e-up e2e-down e2e-status e2e-logs e2e \
        obs-up obs-down obs-clean obs-status obs-logs \
        k6-smoke k6-load k6-stress

# Pin the Go toolchain for all recipes. The locally installed go1.27.1-X build
# writes export data format v4, which no released golang.org/x/tools reads
# (max v3), breaking mockery/golangci-lint/gopls. Released go1.26.4 writes v2.
GOTOOLCHAIN ?= go1.26.4
export GOTOOLCHAIN

# End-to-end stack. Host ports are deliberately different from docker-compose.yml
# so the e2e stack can run alongside `make dev`.
E2E_COMPOSE := docker/docker-compose.e2e.yml
E2E_KEYS    := docker/e2e/keys

# Observability stack (logs only). Standalone compose file; `make dev` does not
# depend on it. Logs written here are tailed by the collector's file_log receiver
# and shipped to Loki without any application change.
OBS_COMPOSE := docker/docker-compose.obs.yml
LOGS_DIR    := logs

# Default target
help:
	@echo "TicketSaas development commands:"
	@echo "  make infra-up              Start Docker infrastructure"
	@echo "  make infra-down            Stop Docker infrastructure"
	@echo "  make infra-logs            Tail Docker logs"
	@echo "  make infra-clean           Stop and remove volumes"
	@echo ""
	@echo "  make dev-auth              Run auth service"
	@echo "  make dev-ticketing         Run ticketing service"
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
	@echo "  make test-fast             Run tests without race detector"
	@echo "  make test-coverage         Run tests with coverage report"
	@echo "  make test-race             Run concurrency tests x10"
	@echo "  make integration-test      Run integration tests (needs Docker)"
	@echo "  make integration-test-race Run integration tests with race detector"
	@echo "  make mocks                 Regenerate mockery mocks"
	@echo "  make lint                  Run Go linter"
	@echo "  make format                Format Go code"
	@echo ""
	@echo "  make e2e-up                Start the e2e stack (Traefik on :8100)"
	@echo "  make e2e-down              Stop the e2e stack and delete its volumes"
	@echo "  make e2e-status            Show e2e container status"
	@echo "  make e2e-logs              Tail e2e logs"
	@echo "  make e2e                   Start the e2e stack, run tests/e2e, tear it down"
	@echo ""
	@echo "  make obs-up                Start collector + Tempo + Loki + Prometheus + Grafana"
	@echo "  make obs-down              Stop the observability stack"
	@echo "  make obs-clean             Stop it and delete the Tempo/Loki/Prometheus/Grafana volumes"
	@echo "  make obs-status            Show observability container status"
	@echo "  make obs-logs              Tail collector / Loki / Grafana logs"
	@echo ""
	@echo "  make k6-smoke              Run k6 smoke test (5 VUs, 1m)"
	@echo "  make k6-load               Run k6 load test (50 VUs, 5m)"
	@echo "  make k6-stress             Run k6 stress test (10→300 VUs, 10m)"

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
	$(MAKE) dev-ticketing & \
	$(MAKE) dev-payment & \
	$(MAKE) dev-web & \
	wait

# Services
# .env and .envrc live next to the entrypoint. direnv exec loads them but keeps
# the working directory at the repo root, so the package path stays root-relative.
#
# Output is teed to $(LOGS_DIR)/<service>.log so the observability collector can
# tail it. `bash -o pipefail` is required: without it the pipeline's exit status
# is tee's, and a crashed service would still report success. Make's default
# shell is /bin/sh, which is dash on Debian/Ubuntu and has no pipefail.
dev-auth:
	@mkdir -p $(LOGS_DIR)
	@bash -o pipefail -c 'direnv exec service/auth/cmd/api go run ./service/auth/cmd/api 2>&1 | tee $(LOGS_DIR)/auth.log'

dev-ticketing:
	@mkdir -p $(LOGS_DIR)
	@bash -o pipefail -c 'direnv exec service/ticketing/cmd/api go run ./service/ticketing/cmd/api 2>&1 | tee $(LOGS_DIR)/ticketing.log'

dev-payment:
	@mkdir -p $(LOGS_DIR)
	@bash -o pipefail -c 'direnv exec service/payment/cmd/api go run ./service/payment/cmd/api 2>&1 | tee $(LOGS_DIR)/payment.log'

dev-web:
	pnpm run dev

direnv-allow:
	direnv allow service/auth/cmd/api
	direnv allow service/ticketing/cmd/api
	direnv allow service/payment/cmd/api

# Builds
build: build-auth build-ticketing build-payment

build-auth:
	@mkdir -p bin
	go build -o bin/auth-service ./service/auth/cmd/api

build-ticketing:
	@mkdir -p bin
	go build -o bin/ticketing-service ./service/ticketing/cmd/api

build-payment:
	@mkdir -p bin
	go build -o bin/payment-service ./service/payment/cmd/api

clean:
	rm -rf bin

# Docker image builds
# SERVICE is the service name, matching the build path in docker/Dockerfile.
docker-build-auth:
	docker build -f docker/Dockerfile --build-arg SERVICE=auth -t ticketbus/auth-service:latest .

docker-build-ticketing:
	docker build -f docker/Dockerfile --build-arg SERVICE=ticketing -t ticketbus/ticketing-service:latest .

docker-build-payment:
	docker build -f docker/Dockerfile --build-arg SERVICE=payment -t ticketbus/payment-service:latest .

docker-build: docker-build-auth docker-build-ticketing docker-build-payment

# Docker migration image build (single image for all services)
docker-migrate-build:
	docker build -f docker/migrate.Dockerfile -t ticketbus/ticketbus-migrations:latest .

# Docker migration run (requires DB to be running, --network=host for localhost access)
docker-migrate-run-auth:
	docker run --rm --network=host ticketbus/ticketbus-migrations auth "$(DATABASE_URL_AUTH)" up

docker-migrate-run-ticketing:
	docker run --rm --network=host ticketbus/ticketbus-migrations ticketing "$(DATABASE_URL_TICKETING)" up

docker-migrate-run-payment:
	docker run --rm --network=host ticketbus/ticketbus-migrations payment "$(DATABASE_URL_PAYMENT)" up

docker-migrate-run: docker-migrate-run-auth docker-migrate-run-ticketing docker-migrate-run-payment

# End-to-end stack (docker/docker-compose.e2e.yml)
# Brings up zookeeper, kafka, three postgres instances, runs all migrations,
# starts auth/ticketing/payment, and puts Traefik in front on :8100 so tests can
# exercise the real forward-auth gateway. Reuses docker/migrate.Dockerfile for
# the one-shot migration steps.

# e2e-keys generates the RSA keypair the auth service signs and verifies with.
# Only auth needs it — ticketing and payment trust the X-Authenticated-* headers
# Traefik injects, never a JWT — so the files stay inside the auth container via
# an entrypoint that exports them. Idempotent.
e2e-keys:
	@mkdir -p $(E2E_KEYS)
	@test -f $(E2E_KEYS)/jwt_private.pem || \
		openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 \
			-out $(E2E_KEYS)/jwt_private.pem 2>/dev/null
	@test -f $(E2E_KEYS)/jwt_public.pem || \
		openssl pkey -in $(E2E_KEYS)/jwt_private.pem -pubout \
			-out $(E2E_KEYS)/jwt_public.pem 2>/dev/null
	@echo "e2e keys ready in $(E2E_KEYS)"

e2e-up: e2e-keys
	docker compose -f $(E2E_COMPOSE) up -d --build --wait

e2e-down:
	docker compose -f $(E2E_COMPOSE) down -v

e2e-status:
	docker compose -f $(E2E_COMPOSE) ps

e2e-logs:
	docker compose -f $(E2E_COMPOSE) logs -f

# e2e brings the stack up, runs the black-box suite in tests/e2e, and always
# tears the stack down again — preserving the test exit status.
e2e: e2e-up
	@go -C tests/e2e test -tags=e2e -count=1 -timeout 20m -v ./...; \
	status=$$?; \
	echo; echo "==> tearing down e2e stack"; \
	$(MAKE) --no-print-directory e2e-down >/dev/null; \
	exit $$status

# Observability (logs only). The collector tails $(LOGS_DIR)/*.log written by the
# dev-* targets and ships them to Loki, so log aggregation needs no application
# change. Grafana is published on :3300 because the Next.js dev server already
# occupies :3000.
#
# obs-up creates $(LOGS_DIR) before starting the collector on purpose: the
# collector bind-mounts it, and if the host path does not exist Docker creates it
# owned by root — which then stops the host-side `tee` in the dev-* targets from
# writing into it.
obs-up:
	@mkdir -p $(LOGS_DIR)
	docker compose -f $(OBS_COMPOSE) up -d
	@echo "==> waiting for loki, tempo and prometheus"
	@for i in $$(seq 1 90); do \
		if curl -fsS http://localhost:3100/ready >/dev/null 2>&1 \
			&& curl -fsS http://localhost:3200/ready >/dev/null 2>&1 \
			&& curl -fsS http://localhost:9097/-/ready >/dev/null 2>&1; then \
			echo "loki, tempo and prometheus ready"; break; \
		fi; \
		if [ $$i -eq 90 ]; then echo "a backend did not become ready"; exit 1; fi; \
		sleep 1; \
	done
	@echo "grafana      http://localhost:3300  (admin/admin)"
	@echo "prometheus   http://localhost:9097"
	@echo "tempo        http://localhost:3200"
	@echo "loki         http://localhost:3100"

obs-down:
	docker compose -f $(OBS_COMPOSE) down

# Discards the Loki index and Grafana state. Also the way to clear the duplicate
# lines that appear in Loki after `make obs-up` re-reads the retained log files.
obs-clean:
	docker compose -f $(OBS_COMPOSE) down -v
	rm -rf $(LOGS_DIR)

obs-status:
	docker compose -f $(OBS_COMPOSE) ps

obs-logs:
	docker compose -f $(OBS_COMPOSE) logs -f

# Migrations. Each connection string is read from that service's .env through
# direnv, so the .env stays the single source of truth and no URL is duplicated
# here. Run `make direnv-allow` once first, otherwise direnv refuses to load the
# .env and these expand to empty.
# Override for a one-off run by exporting the variable:
#   DATABASE_URL_AUTH=postgres://... make migrate-auth-up
DATABASE_URL_AUTH      ?= $(shell direnv exec service/auth/cmd/api sh -c 'echo $$DATABASE_URL')
DATABASE_URL_TICKETING ?= $(shell direnv exec service/ticketing/cmd/api sh -c 'echo $$DATABASE_URL')
DATABASE_URL_PAYMENT   ?= $(shell direnv exec service/payment/cmd/api sh -c 'echo $$DATABASE_URL')

migrate-auth-up:
	migrate -path migrations/auth -database "$(DATABASE_URL_AUTH)" up

migrate-ticketing-up:
	migrate -path migrations/ticketing -database "$(DATABASE_URL_TICKETING)" up

migrate-payment-up:
	migrate -path migrations/payment -database "$(DATABASE_URL_PAYMENT)" up

migrate: migrate-auth-up migrate-ticketing-up migrate-payment-up

# Testing
test:
	go test -race -count=1 ./...

test-fast:
	go test -count=1 ./...

test-verbose:
	go test -race -count=1 -v ./...

test-coverage:
	go test -race -short -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	@echo "✅ HTML report: open coverage.html"
	go tool cover -html=coverage.out -o bin/coverage.html

test-race:
	go test -race -count=10 ./service/payment/internal/application/... ./service/ticketing/internal/application/...

# Mock generation
mocks:
	mockery

# Integration tests (testcontainers-go — needs Docker daemon).
# Each suite boots its own Postgres (plus Kafka, except auth), applies only its
# own migrations, and imports nothing from the other services.
#
# The suites run sequentially and fail fast: the first failure aborts the loop,
# so the remaining suites are skipped. Make's -j does not help here because all
# three live in a single recipe; running them concurrently would require
# splitting them into separate targets.
INTEGRATION_SUITES := service/auth/tests/integration service/ticketing/tests/integration service/payment/tests/integration

integration-test:
	@for suite in $(INTEGRATION_SUITES); do \
		echo "==> $$suite"; \
		go test -tags=integration -count=1 -v ./$$suite/... || exit 1; \
	done

integration-test-race:
	@for suite in $(INTEGRATION_SUITES); do \
		echo "==> $$suite"; \
		go test -tags=integration -race -count=1 -v ./$$suite/... || exit 1; \
	done

# Linting
lint:
	golangci-lint run ./...

format:
	gofmt -w .

# k6 load testing (requires k6: https://k6.io/docs/get-started/installation/)
K6 ?= k6
TARGET_HOST ?= http://localhost:8000

k6-smoke:
	TARGET_HOST=$(TARGET_HOST) $(K6) run apps/k6/smoke.js

k6-load:
	TARGET_HOST=$(TARGET_HOST) $(K6) run apps/k6/load.js

k6-stress:
	TARGET_HOST=$(TARGET_HOST) $(K6) run apps/k6/stress.js
