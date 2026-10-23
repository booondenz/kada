# Kada

**English** | [简体中文](README.zh.md)

A short link management and analytics platform. Supports link folders and tags, custom domains, access passwords, expiry times, UTM templates, a click analytics dashboard, and an AI assistant that answers questions about your own links.

The backend is written in Go, the frontend is a Vite single-page app served as static files, and the AI assistant runs inside the API process.

## Features

- No sign-in wall: opening the site starts a guest session (JWT) and everything is usable immediately.
  Phone number + password is the identity a guest account is claimed with - the API routes and their tests
  exist, the claim screen does not yet
- Create, edit, delete, and bulk-manage short links
- Custom short codes and custom domains
- Access passwords and expiry times
- Folders and tags
- UTM parameters and templates
- Link preview, QR codes, and CSV export
- Click analytics dashboard (overview, platform breakdown, daily trend, visitor detail)
- Open API tokens
- AI assistant (bundled product documentation in the prompt, tool calls that act as the signed-in user)
- Light and dark themes

## Tech stack

| Layer | Technology |
|-------|------------|
| Backend | Go 1.26, Gin, GORM |
| Storage | PostgreSQL 16, Redis 7 |
| Frontend | Vite, React 19, React Router, TypeScript, SWR, Tailwind |
| AI assistant | Go (Eino), DeepSeek |
| Deployment | Docker Compose, Nginx, systemd, GitHub Actions |

## Architecture

Layered as Handler -> Service -> Infra, as a modular monolith.

A click is recorded by a goroutine of its own, so the redirect never waits for a database write. It used to
be published to Kafka and written by a second binary; that pipeline was never deployed (the host had no
broker and no worker unit), so it was removed and there is now one write path.

The database schema is defined by the GORM models in `internal/domain/entity` and applied by
`cmd/migrate` (AutoMigrate). There are no SQL migration files: the structs are the single source
of truth, and AutoMigrate only adds missing tables/columns/indexes, so it is safe to re-run.

The AI assistant runs inside the API process: it serves `/api/ai/*` itself, keeps its conversations in
the same PostgreSQL as everything else, carries its product documentation compiled into the binary, and
calls the application's own services for the tools it offers. See the deployment section of
[docs/design.md](docs/design.md).

## Getting started

Requirements: Go 1.26+, Node 22+, Docker.

```bash
cp .env.example .env
docker compose up -d
```

Local development:

```bash
go run ./cmd/migrate/         # schema only - the API applies it on startup
go run ./cmd/server/main.go   # 8080
cd web && npm run dev         # 3000
```

## Measured baseline

`cmd/loadgen` drives the redirect path of a running instance and reads the cache counters back from
`/metrics`: a cache hit is invisible to the client, so the server's own counters are the only witness. It
seeds its own links and runs two scenarios - a working set of codes that exist, and a stream of codes that
do not.

```bash
go run ./cmd/migrate           # schema only; the API applies it on startup too
go run ./cmd/server            # PostgreSQL and Redis

# in another shell
go run ./cmd/loadgen -codes 200 -absent 2000 -requests 20000 -clients 4000 -concurrency 50
```

| scenario | requests | throughput | p50 | p99 | cache hit ratio |
|---|---|---|---|---|---|
| working set: 200 codes that exist | 20,000 | 1,610 req/s | 20.9 ms | 124 ms | 100% |
| absent codes: 2,000 that do not exist | 20,000 | 5,105 req/s | 6.5 ms | 56.7 ms | 90% |

Measured on one laptop (12 logical CPUs, Windows), the API, PostgreSQL 16 and Redis 7 on the same machine,
50 concurrent clients spread over 4,000 synthetic addresses (sent as `X-Real-IP`, so that the per-address
rate limiter does not shape the result), and 2,000 warm-up requests before the working set is measured. The
numbers are from one run: throughput reproduces within a few percent, the tail moves by tens of milliseconds.

Two things in that table are not about the cache:

- **The two rows differ by the click write.** A 302 records a click; an absent code records nothing, because
  there is no link to attach it to. Every click is an insert and a counter update in one transaction, at
  roughly 1,600-1,800 a second, and that is what the working set is limited by.
- **Neither the cache nor the working set is the limit.** Raising the working set to 2,000 codes moved
  throughput from 1,610 to 1,776 req/s. The 90% in the second row is the first pass, in which 2,000 codes
  reach PostgreSQL once each; every request after that was a hit.

## Environment variables

| Variable | Description |
|----------|-------------|
| JWT_SECRET | Required, a strong random secret |
| POSTGRES_PASSWORD | Required, must be changed in production |
| DB_AUTO_MIGRATE | Let the API server apply the schema on startup (default true; set false when using cmd/migrate) |
| SMS_ACCESS_KEY_ID / SMS_ACCESS_KEY_SECRET | Alibaba Cloud SMS. Required to claim a guest account with a phone number; opening the app does not need it |
| SMS_SIGN_NAME / SMS_TEMPLATE_CODE | The system-granted SMS signature and template from the Aliyun PNVS console |
| DEEPSEEK_API_KEY | The assistant's chat model key |

In production these come from GitHub repository secrets and are written into `/opt/kada/backend/.env` by
the deploy job; the deployment section of [docs/design.md](docs/design.md) explains what that job does.

## Layout

```text
cmd/       entry points: the API, the migration and the load generator
config/    environment parsing (the single source of configuration)
internal/  the application: domain, handler, service, infra, middleware, metrics, assistant
web/       Vite single-page app, built to static files and served by nginx
docs/      Design, contributing and code-style documentation
deploy/    Deployment scripts, and deploy/nginx/ with the reverse proxy configuration
scripts/   Maintenance scripts and the deploy-step test harness
go.mod     the module is the repository root: there is no backend/ directory to cd into
docker-compose.yml
```
