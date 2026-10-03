# TicketSaas

A ticketing platform built with Go microservices, Kafka, PostgreSQL, Traefik, and Next.js.

## Architecture

```
Browser (Next.js :3000)
  │
  ▼
Traefik (:8000) — CORS, rate-limit, forward-auth (→ auth:8081/api/auth/verify)
  │
  ├─ /api/auth/*                  → auth-service     :8081  (JWT, RBAC, registration)
  ├─ /api/events/*                ┐
  ├─ /api/admin/events/*          ┘→ ticketing-service :8082  (events, approvals, reservations, bookings)
  └─ /api/payments/*              → payment-service   :8083  (transactions, gateway sessions, webhooks)

Services communicate asynchronously via Kafka (4 brokers, RF=3, 4 partitions per topic):
  ├─ event.cancelled    ticketing → ticketing (event status cache) + payment (refund requests)
  ├─ payment.completed  payment   → ticketing (confirm booking, issue tickets)
  ├─ payment.expired    payment   → ticketing (expire booking, release seats)
  └─ ticket.issued      ticketing → (emitted per booking item on confirmation)

Seat availability is authoritative in PostgreSQL:
  └─ ticket_types.available_seat — atomic seat counter (conditional UPDATE ... WHERE available_seat >= qty)

Reservation expiry lives in the payment service:
  └─ gateway webhook payment_session.expired (primary)
  └─ in-process expiry poller, EXPIRY_POLL_SEC (fallback)
```

## Roles

| Role | Capabilities |
|------|-------------|
| `customer` | Browse published events, reserve tickets, pay via gateway, view own bookings |
| `eo` (organizer) | Create events (pending), view own events, update/cancel own events, reprocess cancellation |
| `admin` | Approve/reject pending events, view all events, cancel/reprocess any event, seeded from `ADMIN_SEEDS` |

## Transactional Outbox

All inter-service communication flows through the **transactional outbox pattern** — each service writes domain events to an outbox table in its own database within the same transaction as the business data. A background worker polls every `OUTBOX_POLL_MS` (200ms), publishes undelivered messages to Kafka in batches of 100, and marks them `delivered=true`.

```
┌──────────────┐     ┌──────────────┐     ┌──────────┐
│  Business Tx │────▶│ Outbox Table │────▶│  Worker  │──▶ Kafka
│ (atomic)     │     │ (same DB)    │     │ (200ms)  │
└──────────────┘     └──────────────┘     └──────────┘
```

| Service | Produces via Outbox | Consumes |
|---------|-------------------|----------|
| Auth | — | — |
| Ticketing | `event.cancelled`, `ticket.issued` | `payment.completed`, `payment.expired` |
| Payment | `payment.completed`, `payment.expired` | `event.cancelled` |

This guarantees **at-least-once delivery** and eliminates the dual-write problem (no DB write that can succeed while the Kafka write fails). Consumers are idempotent via guarded status transitions (`TransitionIfActive`, `UpdateStatusIfPending`), so a redelivered message is a no-op instead of a double write.

## Services

| Service | Language | Port | Database | Responsibilities |
|---------|----------|------|----------|-----------------|
| `auth-service` | Go | 8081 | `auth_db` (5432) | Customer/organizer registration, login, JWT (access 15m, refresh 7d), Traefik ForwardAuth `verify`, organizer profiles, admin seed from `ADMIN_SEEDS` |
| `ticketing-service` | Go | 8082 | `ticketing_db` (5433) | Event CRUD, admin approval workflow, live seat counters, reservation holds (`RESERVATION_TTL`), booking transitions, cancel cascade, outbox: `event.cancelled`, `ticket.issued` |
| `payment-service` | Go | 8083 | `payment_db` (5435) | Transaction lifecycle (`initiated → pending → completed \| expired`), gateway session creation via pluggable `mock`/`xendit` processor, webhook ingestion, expiry poller, refund requests, outbox: `payment.*` |
| `web` | Next.js 16 | 3000 | — | TanStack Query data fetching, `proxy.ts` route guards, ticket-stub UI, toasts, ConfirmDialog, Tooltip, SSR homepage |

All amounts are stored and sent to the gateway as whole rupiah integers (`amount_rupiah`, `price_rupiah`, currency `IDR`) — no minor-unit conversion anywhere in the payment path.

## Event Lifecycle

```
EO creates event → status=pending
  ↓
Admin approves → status=published + seat counters initialised per ticket type (synchronous)
  ↓
Customer browses → GET /api/events (published only)
  ↓
Customer reserves → POST /api/bookings/reserve
    seats decremented in PostgreSQL, booking created (status=pending, expires_at = now + RESERVATION_TTL)
    payment transaction created synchronously via POST /api/payments/internal (rollback if it fails)
  ↓
Customer pays → POST /api/payments/booking/{booking_id}
    gateway session created (status=pending, provider_ref + payment_link_url stored)
    → frontend opens the hosted payment page in a new tab and polls GET /api/payments/booking/{booking_id}
  ↓
┌─ payment_session.completed  → status=completed → outbox: payment.completed → booking confirmed → ticket.issued per item
└─ payment_session.expired    → status=expired   → outbox: payment.expired   → booking expired, seats released
```

`ProcessPayment` refuses to start a session when the transaction has less than a minute of life left, and clamps the gateway expiry to at least 10 minutes in the future (gateway minimum) while never exceeding `expires_at - GATEWAY_EXPIRY_BUFFER_MIN`. A session can only be created once — the `initiated → pending` transition is guarded, so a duplicate call returns `409` and voids the orphaned gateway session.

### Reservation expiry (race-safe)

```
Gateway webhook payment_session.expired → HandleSessionWebhook → outbox: payment.expired
Fallback: in-process expiry poller (EXPIRY_POLL_SEC) cancels the gateway session, marks the
          transaction expired, and emits payment.expired with reason=internal_expiry_fallback
  ↓
Ticketing consumes payment.expired → booking status=expired → seats released

Race: payment.completed and payment.expired may arrive concurrently. Both transitions are
guarded (TransitionIfActive / UpdateStatusIfPending), so exactly one wins and the other is a
no-op — seats are released at most once.
```

A `payment_session.completed` that arrives after the transaction already expired does not confirm
the booking. It opens a late-payment refund instead (see below).

### Event cancel & refund flow

```
EO (or admin) cancels → POST /api/events/{id}/cancel → outbox: event.cancelled
  ↓
Ticketing consumes event.cancelled → cache event status → cancel bookings + release seats
  ↓
Payment consumes event.cancelled → for every completed transaction of that event:
    INSERT refund_requests (status=pending, reason=event_cancelled,
                           idempotency_key='event-cancelled:'||txn_id, provider_ref)
    UPDATE transactions SET refund_status='pending'
    Transactions still initiated or pending are left alone — the customer may still pay, and the
    session expires on its own.
  ↓
Anything that completed after that pass is picked up by a later republish:
POST /api/events/{id}/cancel-reprocess (EO) or POST /api/admin/events/{id}/cancel-reprocess (admin)
  ↓
A payment completed after its transaction already expired never confirms the booking — it opens a
late-payment refund (reason=late_payment) instead
```

Refunds are tracked entirely in the payment service — the bookings table carries no refund column,
and totals are derived from `booking_items.total_price_rupiah`.

| `transactions.refund_status` | Meaning |
|-----------------------------|---------|
| `none` | No refund needed |
| `pending` | Refund owed (event cancelled after payment completed, or late payment) |

| `refund_requests.status` | Meaning |
|--------------------------|---------|
| `pending` | Refund recorded, not yet sent to the provider |
| `processing` | Provider call in flight |
| `succeeded` | Provider confirmed the refund |
| `failed` | Provider rejected the refund |

## Quick Start

### Prerequisites

- Go 1.26+, Node.js 20+, pnpm 9, Docker, [direnv](https://direnv.net/), and the [`migrate`](https://github.com/golang-migrate/migrate) CLI

```bash
# 1. Allow direnv (loads per-service .env files)
make direnv-allow

# 2. Start infrastructure (PostgreSQL ×3, Kafka ×4 + Zookeeper, Traefik, Kafka UI)
make infra-up

# 3. Run database migrations
make migrate

# 4. Start all services (3 backends + frontend)
make dev
```

Admin users are seeded from `ADMIN_SEEDS` on first start; an empty value skips seeding.

### Run services individually

```bash
make dev-auth        # Auth service on :8081
make dev-ticketing   # Ticketing service on :8082
make dev-payment     # Payment service on :8083
make dev-web         # Next.js on :3000
```

### Build binaries

```bash
make build           # → bin/auth-service, bin/ticketing-service, bin/payment-service
```

## Environment

Each service has its own `.env` file under `cmd/<service>/` loaded by `direnv`. Every variable
below is required at startup unless marked optional.

| Service | Key env vars |
|---------|-------------|
| auth | `APP_ENV`, `PORT`, `DATABASE_URL`, `JWT_PRIVATE_KEY`, `JWT_PUBLIC_KEY`, `ADMIN_SEEDS` (optional, JSON array) |
| ticketing | `APP_ENV`, `PORT`, `DATABASE_URL`, `KAFKA_BROKERS`, `RESERVATION_TTL`, `CONSUMER_CONCURRENCY`, `OUTBOX_CONCURRENCY`, `OUTBOX_POLL_MS`, `PAYMENT_SERVICE_URL`, `PAYMENT_TIMEOUT_SEC`, `INTERNAL_API_KEY` |
| payment | `APP_ENV`, `PORT`, `DATABASE_URL`, `KAFKA_BROKERS`, `WEBHOOK_BASE_URL`, `PROVIDER`, `XENDIT_BASE_URL`, `WEBHOOK_CALLBACK_TOKEN`, `CONSUMER_CONCURRENCY`, `OUTBOX_CONCURRENCY`, `OUTBOX_POLL_MS`, `GATEWAY_EXPIRY_BUFFER_MIN`, `EXPIRY_POLL_SEC`, `INTERNAL_API_KEY`, `XENDIT_API_KEY` (required only when `PROVIDER=xendit`), `PAYMENT_METHODS` (optional) |

Notable values:

| Key | Service | Meaning |
|-----|---------|---------|
| `PROVIDER` | payment | `mock` (deterministic in-process sessions) or `xendit` (Payment Sessions API) |
| `WEBHOOK_CALLBACK_TOKEN` | payment | Shared `x-callback-token` secret required on every webhook, both providers |
| `RESERVATION_TTL` | ticketing | Seat-hold lifetime in seconds; must be > 0. Also becomes the transaction expiry |
| `GATEWAY_EXPIRY_BUFFER_MIN` | payment | Gateway session is set to `expires_at - buffer`, floored at the provider minimum |
| `EXPIRY_POLL_SEC` | payment | Interval of the fallback expiry poller |
| `INTERNAL_API_KEY` | ticketing, payment | `X-Internal-Key` for service-to-service calls (reserve → payment) |

## Ports

| Component | Port | Notes |
|-----------|------|-------|
| Traefik API Gateway | 8000 | All `/api/*` traffic |
| Traefik Dashboard | 8085 | http://localhost:8085 |
| Auth Service | 8081 | JWT, ForwardAuth verify, `/api/auth/health` |
| Ticketing Service | 8082 | Events + bookings, `/api/events/health`, `/api/bookings/health` |
| Payment Service | 8083 | Webhooks + gateway sessions, `/api/payments/health` |
| Next.js Frontend | 3000 | http://localhost:3000 |
| Kafka UI | 8080 | http://localhost:8080 |
| Kafka 1–4 | 9092–9095 | External (host), 29092–29095 internal (Docker) |
| Zookeeper | 2181 | Kafka coordination |
| PostgreSQL auth | 5432 | `auth_db` |
| PostgreSQL ticketing | 5433 | `ticketing_db` |
| PostgreSQL payment | 5435 | `payment_db` |
| Grafana (observability) | 3300 | http://localhost:3300 — `admin`/`admin` |
| Tempo (trace query API) | 3200 | http://localhost:3200 |
| Loki (log query API) | 3100 | http://localhost:3100 |
| OTel collector OTLP | 4317 / 4318 | gRPC / HTTP, both published |

Grafana is on **3300**, not 3000, because the Next.js dev server already binds 3000.
The e2e stack offsets the observability ports (13200, 14318) so both stacks can run
at once.

OpenTelemetry metrics (replacing the former `/metrics` endpoint) are not wired up yet.

## Observability

An [OpenTelemetry Collector](https://opentelemetry.io/) sits in front of Tempo (traces)
and Loki (logs), with Grafana on top for both.

```bash
make obs-up      # collector + Tempo + Loki + Grafana
make obs-status  # container status
make obs-logs    # tail the stack
make obs-down    # stop, keep data
make obs-clean   # stop and delete the Tempo/Loki/Grafana volumes
```

| UI | URL | Credentials |
|---|---|---|
| Grafana | http://localhost:3300 | `admin` / `admin` |
| Tempo | http://localhost:3200 | — |
| Loki | http://localhost:3100 | — |

### Traces

Tracing is **off unless `OTEL_EXPORTER_OTLP_ENDPOINT` is set** — with the variable
unset the SDK installs no tracer provider and every span is a no-op, which keeps
the test suites free of telemetry overhead. To enable it locally, add this to each
`service/<svc>/cmd/api/.env`:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
```

The e2e stack sets it for you. The standard `OTEL_*` variables are honoured
throughout, so pointing at a hosted backend later needs no code change.

A booking produces **two** traces, because the reservation and the payment
webhook are separate inbound requests:

```
POST /api/bookings/reserve          SERVER   ticketing-service
├─ SELECT / INSERT / pool.acquire   CLIENT   postgres
└─ POST /api/payments/internal      CLIENT   ticketing-service
   └─ SERVER /api/payments/internal SERVER   payment-service
      └─ SELECT / INSERT            CLIENT   postgres
```

```
POST /api/payments/webhook          SERVER   payment-service
└─ publish payment.completed        PRODUCER payment-service   ← parent restored from outbox.trace_context
   └─ payment.completed process     CONSUMER ticketing-service
      └─ UPDATE / INSERT            CLIENT   postgres
      └─ publish ticket.issued      PRODUCER ticketing-service
```

The two are tied together by `booking_id` / `txn_id` span attributes rather than
being one trace. `tests/e2e/trace_test.go` asserts both shapes, including the
cross-service parent links.

```bash
# list recent traces, then dump one with its span/parent IDs per service
curl -s -G localhost:3200/api/search --data-urlencode 'tags=service.name=payment-service'
curl -s "localhost:3200/api/traces/<traceID>" | jq -r '
  .batches[].scopeSpans[].spans[]
  | "\(.name)\tspan=\(.spanId)\tparent=\(.parentSpanId)"'
```

Note: omit `start`/`end` on `/api/traces/<id>` — supplying a range restricts the
search and can return a partial or missing trace.

Every HTTP access log line carries the `trace_id` of its request, so Grafana's
trace view can jump straight to the matching log lines.

### Logs

Log aggregation needs **no application change** — the services keep using `slog`
and the collector tails the files written by the `dev-*` targets.

```bash
make dev-ticketing ──> logs/ticketing.log ──> collector (file_log) ──> Loki ──> Grafana
```

`make dev-*` tees each service's stdout **and stderr** into `logs/<service>.log`; the
collector tails those files. Because `slog` emits JSON, the collector promotes `service`
to the `service_name` index label and keeps every other field as queryable metadata.

Loki distinguishes **index labels** (stream selectors, low cardinality) from **structured
metadata** (everything else). Only `service_name` is a label, so filter on fields in the
pipeline, not the selector:

```logql
{service_name="ticketing-service"}                        # by label
{service_name="ticketing-service"} | level="ERROR"        # by field
{service_name="ticketing-service"} | booking_id="abc123"  # by domain id
```

### Known limitations

- **`trace_id` reaches HTTP access logs, not every log line.** `RequestLogger` logs with
  `InfoContext`, so each access log carries its request's `trace_id`. The other ~171
  `slog` call sites use the non-`ctx` variants and therefore have no trace attached.
  Application-level logs are still reachable from a trace via the `booking_id` /
  `txn_id` span attributes.
- **HTTP access logs are structured JSON.** `sharedhttp.RequestLogger` replaced chi's
  `middleware.Logger`, which wrote plain text the collector could not index. Each request
  now emits one slog line carrying `method`, `path` (the chi route pattern, so
  `/api/bookings/{id}` stays one value), `status`, `bytes`, `duration_ms`, and `request_id`.
  `/health` is skipped so container healthchecks do not flood the index.
- **Request counters and latency histograms are gone.** Prometheus instrumentation was
  removed along with the `/metrics` endpoint; OpenTelemetry metrics are the intended
  replacement but are not wired up yet. Until then, `status` and `duration_ms` in the
  access log are the substitute.
- **Non-JSON lines are dropped on purpose.** `2>&1` means the file also receives direnv's
  "loading .envrc" notice and `exit status N` from `go run`. Without `on_error: drop`, the
  first such line fails `json_parser` and the collector stops reading that file entirely,
  taking every valid line with it.
- **Restarting the collector re-ingests retained files.** `start_at: beginning` is
  deliberate: with `end`, a service that crashes during startup logs nothing at all.
  Duplicate lines are cheaper than missing ones; `make obs-clean` clears them.
- **Docker container logs are not captured yet.** Only host-run `make dev-*` output is
  tailed. Adding the infra and e2e containers needs a second receiver plus a read-only
  mount of `/var/lib/docker/containers`, and the collector must run as `uid 0` because
  that tree is `root:root` mode `710`. This is the planned phase 1b.
- **Linux host only.** That mount path does not exist on Docker Desktop.

## API Endpoints

### Auth (`/api/auth`)
| Method | Path | Auth | Role |
|--------|------|------|------|
| POST | `/api/auth/register` | No | — |
| POST | `/api/auth/register/organizer` | No | — |
| POST | `/api/auth/login` | No | — |
| POST | `/api/auth/refresh` | No | — |
| GET | `/api/auth/verify` | Bearer | ForwardAuth target for Traefik |
| GET | `/api/auth/me` | Bearer | Any |
| GET | `/api/auth/health` | No | — |

### Events (`/api/events`, `/api/admin/events`)
| Method | Path | Auth | Role |
|--------|------|------|------|
| GET | `/api/events` | No | — |
| GET | `/api/events/{id}` | No | — |
| POST | `/api/events` | Bearer | EO |
| PUT | `/api/events/{id}` | Bearer | EO |
| POST | `/api/events/{id}/cancel` | Bearer | EO (owner) |
| POST | `/api/events/{id}/cancel-reprocess` | Bearer | EO (owner) |
| GET | `/api/events/mine` | Bearer | EO |
| GET | `/api/admin/events` | Bearer | Admin |
| GET | `/api/events/pending` | Bearer | Admin |
| POST | `/api/events/{id}/approve` | Bearer | Admin |
| POST | `/api/events/{id}/reject` | Bearer | Admin |
| POST | `/api/admin/events/{id}/cancel-reprocess` | Bearer | Admin |
| GET | `/api/events/health` | No | — |

### Bookings (`/api/bookings`)
| Method | Path | Auth | Role |
|--------|------|------|------|
| POST | `/api/bookings/reserve` | Bearer | Customer |
| DELETE | `/api/bookings/reserve/{id}` | Bearer | Customer — always responds `409`; seats are released by expiry, not by the customer |
| GET | `/api/bookings` | Bearer | Customer (own bookings) |
| GET | `/api/bookings/{id}` | Bearer | Owner (non-owner gets `404`) |
| GET | `/api/bookings/health` | No | — |

### Payments (`/api/payments`)
| Method | Path | Auth | Role |
|--------|------|------|------|
| POST | `/api/payments/webhook` | `x-callback-token` | — (provider callback, body capped at 1 MB) |
| POST | `/api/payments/booking/{booking_id}` | Bearer | Customer |
| GET | `/api/payments/booking/{booking_id}` | Bearer | Customer |
| GET | `/api/payments/{id}/status` | Bearer | Customer |
| GET | `/api/payments/{txn_id}` | Bearer | Customer |
| GET | `/api/payments` | Bearer | Customer (own transactions) |
| GET | `/api/payments/health` | No | — |
| POST | `/api/payments/internal` | `X-Internal-Key` | ❌ gateway-blocked — service-to-service only |
| POST | `/api/payments/booking/mock/{booking_id}/{status}` | No | ❌ gateway-blocked — dev simulator, `status` = `success` \| `expired` |

The two gateway-blocked routes are excluded by the Traefik payments router
(`!PathPrefix('/api/payments/internal') && !PathPrefix('/api/payments/booking/mock')`) and are
reachable only on the payment service port.

## Testing

### Unit tests (104 tests, 5 packages)

```bash
make test        # go test -race -count=1 ./...
make test-fast   # without race detector
```

All infrastructure is mocked via [mockery](https://github.com/vektra/mockery) (15 generated mocks). No Docker needed.

| Package | Tests |
|---------|-------|
| `auth/application` | 22 |
| `auth/jwt` | 10 |
| `ticketing/application/event` | 30 |
| `ticketing/application/booking` | 19 |
| `payment/application` | 23 |

### Integration tests (39 tests)

```bash
make integration-test      # go test -tags=integration -count=1 -v ./tests/integration/...
make integration-test-race # with race detector
```

Uses [testcontainers-go](https://github.com/testcontainers/testcontainers-go) to spin up one
PostgreSQL 16 container holding all three databases plus one KRaft Kafka broker. Requires a running
Docker daemon. All 3 services are wired in-process and exercised end-to-end — register → login →
create event → approve → reserve → gateway session → webhook → confirm booking, plus expiry, refund,
concurrency, and error-path coverage.

Tests are grouped per area and named `Test<Area>_<Scenario>` (`Auth`, `Event`, `Booking`,
`Payment`, `Webhook`, `Expiry`, `Refund`, `Mock`, `E2E`, `Concurrency`), so a single area can be run
with `go test -tags integration -run 'TestPayment' ./tests/integration/`.

## CI/CD

GitHub Actions on push and pull request to `master` (`.github/workflows/test.yml`):

| Job | Command | Timeout |
|-----|---------|---------|
| Unit Tests | `go test -race -count=1 ./...` | 10 min |
| Integration Tests | `go test -tags=integration -race -count=1 -v ./tests/integration/...` | 15 min |

Both jobs run in parallel on `ubuntu-latest`.

`.github/workflows/deploy.yml` runs after the test workflow completes successfully on a push and
then:

| Job | Timeout | What it does |
|-----|---------|-------------|
| Build & Push | 15 min | Builds one Docker image per backend service from `docker/Dockerfile`, tagged with the `v*` git tag when present, otherwise `latest` plus the short SHA |
| Build & Push migrate image | 10 min | Publishes the migration runner image |

The frontend has no automated deploy. It used to be synced to S3 as a static export, but
`output: 'export'` is no longer enabled in `apps/web/next.config.ts`, so there is no `out/`
directory to upload. Deploy it manually until a hosting target is picked (static S3 export vs a
Node-capable host that can run `next start`).

## Project Structure

```
cmd/                          Service entry points (each with .env, .envrc)
  auth-service/
  ticketing-service/
  payment-service/
internal/
  auth/                       Hexagonal: handler, application, domain, postgres, jwt, bcrypt, config
  ticketing/                  Hexagonal: handler, application (event, booking), domain, postgres,
                              payment (client), kafka, config
  payment/                    Hexagonal: handler, application, domain, postgres, processor
                              (mock, xendit), kafka, config
  shared/                     db, http (context, response helpers, metrics), kafka, outbox,
                              domain (events), log, redis (client)
migrations/
  auth/                       users, refresh tokens, organizers, outbox
  ticketing/                  events, ticket types, bookings, booking items, outbox
  payment/                    transactions, outbox, refund requests
tests/
  integration/                39 integration tests (testcontainers-go), one file per area
  integration/testdata/       Real gateway webhook payload fixtures
apps/
  web/                        Next.js 16 App Router
    src/app/                  /, /login, /register, /dashboard, /events, /events/[id],
                              /checkout/[booking_id], /payment/[txn_id], /confirmation/[id], /admin
    src/components/
      layout/                 Header
      ui/                     Toast, ConfirmDialog, Tooltip
    src/lib/                  api-client, auth-context, admin-api, query-provider (TanStack), format
    src/proxy.ts              Route-level auth guards
    src/types/                TypeScript interfaces
  k6/                         smoke, load, stress scripts (make k6-smoke / k6-load / k6-stress)
docker/                       Docker Compose (Kafka ×4, Zookeeper, PostgreSQL ×3, Traefik, Kafka UI)
                              + Dockerfile, migrate.Dockerfile
traefik/                      Traefik static + dynamic config (routers, middlewares, services)
.github/workflows/            CI: unit + integration tests, then build & deploy
```