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

Services communicate asynchronously via Kafka:
  ├─ Kafka produces: organizer.created · event.{created,approved,rejected,updated,cancelled}
  │                  reservation.{created,expired} · ticket.issued
  │                  payment.{initiated,completed,failed}
  └─ Kafka consumes: inventory listens to event.approved / payment.{completed,failed} / event.cancelled
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
| `admin` | Approve/reject pending events, auto-seeded in dev mode |

## Services

| Service | Language | Port | Database | Responsibilities |
|---------|----------|------|----------|-----------------|
| `auth-service` | Go | 8081 | `auth_db` (5432) | User registration, login, JWT tokens, refresh, ForwardAuth verify, admin seed |
| `event-service` | Go | 8082 | `event_db` (5433) | Event CRUD, organizer profiles, admin approval workflow, seat availability reads |
| `inventory-service` | Go | 8083 | `inventory_db` (5434) | Reservation holds (Redis TTL), booking persistence, seat counter init/reserve/release, expiry listener |
| `payment-service` | Go | 8084 | `payment_db` (5435) | Mock checkout, async webhook (60s delay, 75% success), transaction status |
| `web` | Next.js 16 | 3000 | — | Light-mode UI, DM Serif Display + DM Sans, ticket-stub cards, toast notifications |

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

## Quick Start

### Prerequisites

- Go 1.24+, Node.js 22+, pnpm, Docker, [direnv](https://direnv.net/)

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
apps/web/                     Next.js 16 App Router
  src/
    app/                      Pages: /, /login, /register, /dashboard, /events, /admin, /checkout
    components/
      layout/                 Header
      ui/                     Toast notification system
    lib/                      api-client, auth-context, format, admin-api
    types/                    TypeScript interfaces
docker/                       Docker Compose (Kafka, Redis, PostgreSQL × 4, Traefik, Kafka UI)
traefik/                      Traefik static + dynamic config (routers, middlewares, services)
```
