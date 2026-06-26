# TicketSaas

A ticketing platform built with Go microservices, Kafka, Redis, PostgreSQL, Traefik, and Next.js.

## Architecture

```
Browser (Next.js :3000)
  │
  ▼
Traefik (:8000) — CORS, rate-limit, forward-auth (→ auth:8081/api/auth/verify)
  │
  ├─ /api/auth/*       → auth-service :8081    (JWT, RBAC, registration)
  ├─ /api/events/*     → event-service :8082   (event CRUD, approval)
  ├─ /api/inventory/*  ┐
  ├─ /api/bookings/*   ┘→ inventory-service :8083 (reservations, bookings, seat counters)
  └─ /api/payments/*   → payment-service :8084  (checkout, mock webhook)

Services communicate asynchronously via Kafka (4 brokers, RF=3, 4 partitions per topic):
  ├─ Kafka produces: organizer.created · event.{created,approved,rejected,updated,cancelled}
  │                  reservation.{created,expired} · ticket.issued
  │                  payment.{initiated,completed,failed}
  └─ Kafka consumes: inventory listens to event.{approved,cancelled} / payment.{completed,failed}
                     payment listens to reservation.{created,expired}
                     event listens to organizer.created

Redis:
  ├─ reservation:{booking_id} (TTL 5 min) — transient reservation holds
  ├─ rsvn-data:{booking_id}  (no TTL) — shadow key for expiry recovery
  └─ counter:seat:{event_id}:{ticket_type_id} (no TTL) — atomic seat counters
```

## Roles

| Role | Capabilities |
|------|-------------|
| `customer` | Browse published events, reserve tickets, checkout, view bookings |
| `eo` (organizer) | Create events (pending), view own events, update/cancel own events |
| `admin` | Approve/reject pending events, view all events, auto-seeded in dev mode |

## Transactional Outbox

All inter-service communication flows through the **transactional outbox pattern** — each service writes domain events to an outbox table in its own database within the same transaction as the business data. A background worker polls every 1s, publishes undelivered messages to Kafka, and marks them `delivered=true`.

```
┌──────────────┐     ┌──────────────┐     ┌──────────┐
│  Business Tx │────▶│ Outbox Table │────▶│  Worker  │──▶ Kafka
│ (atomic)     │     │ (same DB)    │     │ (1s poll)│
└──────────────┘     └──────────────┘     └──────────┘
```

| Service | Produces via Outbox | Consumes |
|---------|-------------------|----------|
| Auth | `organizer.created` | — |
| Event | `event.{created,approved,rejected,updated,cancelled}` | `organizer.created` |
| Inventory | `reservation.{created,expired}` | `event.{approved,cancelled}`, `payment.{completed,failed}` |
| Payment | `payment.{initiated,completed,failed}` | `reservation.{created,expired}` |

This guarantees **at-least-once delivery** and eliminates the dual-write problem (no DB write that can succeed while the Kafka write fails). Consumers are idempotent via `ON CONFLICT DO NOTHING` and cache-miss-safe handlers.

## Services

| Service | Language | Port | Database | Responsibilities |
|---------|----------|------|----------|-----------------|
| `auth-service` | Go | 8081 | `auth_db` (5432) | Registration, login, JWT (access 15m, refresh 7d), ForwardAuth verify, admin seed, outbox: organizer.created |
| `event-service` | Go | 8082 | `event_db` (5433) | Event CRUD, organizer profiles, admin approval workflow, live seat availability (Redis reader), outbox: event.* |
| `inventory-service` | Go | 8083 | `inventory_db` (5434) | Reservation holds (Redis TTL), booking persistence, seat counter init/reserve/release, expiry listener, cancel cascade with refund tracking, outbox: reservation.* |
| `payment-service` | Go | 8084 | `payment_db` (5435) | Mock checkout, async webhook (60s delay, 75% success), transaction status, idempotency guard (409 on duplicate), outbox: payment.* |
| `web` | Next.js 16 | 3000 | — | TanStack Query data fetching, proxy.ts route auth guards, light-mode ticket-stub UI, toast notifications, ConfirmDialog, SSR homepage |

## Event Lifecycle

```
EO creates event → status=pending
  ↓
Admin approves → status=published → Kafka: event.approved → inventory initializes Redis seat counters
  ↓
Customer browses → GET /api/events (published only)
  ↓
Customer reserves → POST /api/inventory/reserve → Redis counter decremented, reservation with 5min TTL
  ↓
Customer checks out → POST /api/payments/by-booking/{id}/checkout → goroutine waits 60s → mock webhook
  ↓
┌─ Completed (75%) → Kafka: payment.completed → inventory confirms booking → tickets in dashboard
└─ Failed (25%)    → Kafka: payment.failed    → inventory releases seats back to Redis
```

### Reservation expiry (race-safe)

```
Redis TTL expires → keyspace notification → SubscribeExpiry reads shadow key → HandleExpiry releases seats
Race: if payment.completed/failed fires simultaneously, Confirm/Release return nil on cache miss (idempotent)
```

### Event cancel & refund flow

```
EO cancels event → Kafka: event.cancelled
  ↓
Inventory receives → Upsert("cancelled") in event_status_cache
  ↓
CancelByEventID: all bookings → status=cancelled, refund_status=pending
Seats released back to Redis counters
  ↓
Payment already processing? → Confirm() checks IsPublished()
  ├─ false (cancelled) → booking created with status=cancelled, refund_status=pending
  └─ true (published)  → booking confirmed normally, refund_status=none
```

| refund_status | Meaning |
|---------------|---------|
| `none` | No refund needed (confirmed booking, event published) |
| `pending` | Refund owed (event cancelled after payment succeeded) |
| `refunded` | Refund processed |

## Quick Start

### Prerequisites

- Go 1.26+, Node.js 22+, pnpm, Docker, [direnv](https://direnv.net/)

```bash
# 1. Allow direnv (loads per-service .env files)
make direnv-allow

# 2. Start infrastructure (PostgreSQL × 4, Redis, Kafka, Traefik, Kafka UI)
make infra-up

# 3. Run database migrations
make migrate

# 4. Start all services (4 backends + frontend)
make dev
```

The admin user is auto-seeded on first start with `APP_ENV=development`.

### Run services individually

```bash
make dev-auth        # Auth service on :8081
make dev-event       # Event service on :8082
make dev-inventory   # Inventory service on :8083
make dev-payment     # Payment service on :8084
make dev-web         # Next.js on :3000
```

### Build binaries

```bash
make build           # → bin/auth-service, bin/event-service, etc.
```

## Environment

Each service has its own `.env` file under `cmd/<service>/.env` loaded by `direnv`.

| Service | Key env vars |
|---------|-------------|
| auth | `JWT_PRIVATE_KEY`, `JWT_PUBLIC_KEY`, `ADMIN_EMAIL`, `ADMIN_PASSWORD` |
| event | `DATABASE_URL`, `REDIS_ADDR`, `KAFKA_BROKERS` |
| inventory | `DATABASE_URL`, `REDIS_ADDR`, `KAFKA_BROKERS`, `RESERVATION_TTL` |
| payment | `DATABASE_URL`, `WEBHOOK_BASE_URL`, `KAFKA_BROKERS` |

`RESERVATION_TTL` defaults to 300 seconds (5 minutes).

## Ports

| Component | Port | Notes |
|-----------|------|-------|
| Traefik API Gateway | 8000 | All `/api/*` traffic |
| Traefik Dashboard | 8085 | http://localhost:8085 |
| Auth Service | 8081 | JWT, ForwardAuth verify |
| Event Service | 8082 | Event CRUD |
| Inventory Service | 8083 | Reservations, bookings |
| Payment Service | 8084 | Checkout, webhooks |
| Next.js Frontend | 3000 | http://localhost:3000 |
| Kafka UI | 8080 | http://localhost:8080 |
| Kafka | 9092 | External (host), 29092 internal (Docker) |
| Redis | 6379 | Reservations, seat counters |
| PostgreSQL auth | 5432 | `auth_db` |
| PostgreSQL event | 5433 | `event_db` |
| PostgreSQL inventory | 5434 | `inventory_db` |
| PostgreSQL payment | 5435 | `payment_db` |

## API Endpoints

### Auth (`/api/auth`)
| Method | Path | Auth | Role |
|--------|------|------|------|
| POST | `/api/auth/register` | No | — |
| POST | `/api/auth/register/organizer` | No | — |
| POST | `/api/auth/login` | No | — |
| POST | `/api/auth/refresh` | No | — |
| GET | `/api/auth/me` | Bearer | Any |
| GET | `/api/auth/verify` | Bearer | Any (ForwardAuth) |

### Events (`/api/events`)
| Method | Path | Auth | Role |
|--------|------|------|------|
| GET | `/api/events` | No | — |
| GET | `/api/events/{id}` | No | — |
| POST | `/api/events` | Bearer | EO |
| PUT | `/api/events/{id}` | Bearer | EO |
| POST | `/api/events/{id}/cancel` | Bearer | EO |
| GET | `/api/events/mine` | Bearer | EO |
| GET | `/api/events/organizers/me` | Bearer | EO |
| GET | `/api/admin/events` | Bearer | Admin |
| GET | `/api/events/pending` | Bearer | Admin |
| POST | `/api/events/{id}/approve` | Bearer | Admin |
| POST | `/api/events/{id}/reject` | Bearer | Admin |

### Inventory (`/api/inventory`, `/api/bookings`)
| Method | Path | Auth | Role |
|--------|------|------|------|
| POST | `/api/inventory/reserve` | Bearer | Customer |
| DELETE | `/api/inventory/reserve/{id}` | Bearer | Customer |
| GET | `/api/bookings` | Bearer | Customer |
| GET | `/api/bookings/{id}` | Bearer | Any |

### Payments (`/api/payments`)
| Method | Path | Auth | Role |
|--------|------|------|------|
| POST | `/api/payments/by-booking/{id}/checkout` | Bearer | Customer |
| GET | `/api/payments/{id}/status` | Bearer | Customer |
| GET | `/api/payments` | Bearer | Customer |
| POST | `/api/payments/webhook/{provider}` | No | — |

## Testing

### Unit tests (115 tests, 5 packages)

```bash
make test        # go test -race -count=1 ./...
```

All infrastructure is mocked via [mockery](https://github.com/vektra/mockery) (15 generated mocks). No Docker needed.

| Package | Tests |
|---------|-------|
| `auth/application` | 22 |
| `auth/jwt` | 10 |
| `event/application` | 34 |
| `inventory/application` | 26 |
| `payment/application` | 23 |

### Integration tests (25 tests)

```bash
make integration-test      # go test -tags=integration ./tests/integration/...
make integration-test-race # with race detector
```

Uses [testcontainers-go](https://github.com/testcontainers/testcontainers-go) to spin up ephemeral PostgreSQL, Redis, and KRaft Kafka containers. Requires Docker daemon. All 4 services are wired in-process and tested end-to-end — register → login → create event → approve → reserve → checkout → webhook → confirm booking.

## CI/CD

GitHub Actions on push and pull request to `master` (`.github/workflows/test.yml`):

| Job | Command | Timeout |
|-----|---------|---------|
| Unit Tests | `go test -race -count=1 ./...` | 10 min |
| Integration Tests | `go test -tags=integration -race -count=1 -v ./tests/integration/...` | 15 min |

Both jobs run in parallel and are **required status checks** before merge.

## Project Structure

```
cmd/                          Service entry points
  auth-service/               (+ .env, .envrc)
  event-service/
  inventory-service/
  payment-service/
internal/
  auth/                       Hexagonal: handler, application, domain, postgres, jwt, bcrypt, kafka, config
  event/                      Hexagonal: handler, application, domain, postgres, kafka, redis, config
  inventory/                  Hexagonal: handler, application, domain, postgres, redis, kafka, config
  payment/                    Hexagonal: handler, application, domain, postgres, processor, kafka, config
  shared/                     Shared: db, http (middleware, response helpers), kafka, domain (events), log
  shared/outbox/              Transactional outbox: worker, store, migrations
migrations/
  auth/                       Auth DB migrations
  event/                      Event DB migrations
  inventory/                  Inventory DB migrations (bookings, event_status_cache)
  payment/                    Payment DB migrations (transactions)
  shared/outbox/              Outbox table migrations (per-service)
tests/
  fixtures/                   Shared test fixture generators
  integration/                25 integration tests (testcontainers-go)
apps/web/                     Next.js 16 App Router
  src/
    app/                      Pages: /, /login, /register, /dashboard, /events, /checkout, /confirmation
    components/
      layout/                 Header
      ui/                     Toast notifications, ConfirmDialog
    lib/                      api-client, auth-context, query-provider (TanStack), format
    proxy.ts                  Route-level auth guards
    types/                    TypeScript interfaces
docker/                       Docker Compose (Kafka ×4, Redis, PostgreSQL ×4, Traefik, Kafka UI)
traefik/                      Traefik static + dynamic config (routers, middlewares, services)
.github/workflows/            CI: unit + integration tests
```
