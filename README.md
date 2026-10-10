# Kada

**English** | [简体中文](README.zh.md)

A short link management and analytics platform. Supports link folders and tags, custom domains, access passwords, expiry times, UTM templates, a click analytics dashboard, and an AI assistant that answers questions about your own links.

The backend is written in Go, the frontend is a Vite single-page app served as static files, click events are processed asynchronously through Kafka, and the AI assistant runs inside the API process.

## Features

- Phone number + password sign-in (JWT), with the SMS code kept for registration, for accounts that have
  no password yet, and for a forgotten one
- Create, edit, delete, and bulk-manage short links
- Custom short codes and custom domains
- Access passwords and expiry times
- Folders, tags, and workspace management
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
| Messaging | Kafka 3.8 |
| Frontend | Vite, React 19, React Router, TypeScript, SWR, Tailwind |
| AI assistant | Go (Eino), DeepSeek |
| Deployment | Docker Compose, Nginx, systemd, GitHub Actions |

## Architecture

Layered as Handler -> Service -> Infra, using a modular monolith plus a standalone Kafka worker.

Click events are persisted asynchronously through Kafka and increment counters; when Kafka is unavailable this degrades to writing directly to the database.

The database schema is defined by the GORM models in `backend/internal/domain/entity` and applied by
`backend/cmd/migrate` (AutoMigrate). There are no SQL migration files: the structs are the single source
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
cd backend && go run ./cmd/migrate/         # schema only - the API applies it on startup
cd backend && go run ./cmd/server/main.go   # 8080
cd frontend && npm run dev                  # 3000
```

## Environment variables

| Variable | Description |
|----------|-------------|
| JWT_SECRET | Required, a strong random secret |
| POSTGRES_PASSWORD | Required, must be changed in production |
| DB_AUTO_MIGRATE | Let the API server apply the schema on startup (default true; set false when using cmd/migrate) |
| SMS_ACCESS_KEY_ID / SMS_ACCESS_KEY_SECRET | Alibaba Cloud SMS. Required in production: nobody can register (or recover an account) without it |
| SMS_SIGN_NAME / SMS_TEMPLATE_CODE | The system-granted SMS signature and template from the Aliyun PNVS console |
| DEEPSEEK_API_KEY | The assistant's chat model key |

In production these come from GitHub repository secrets and are written into `/opt/kada/backend/.env` by
the deploy job; the deployment section of [docs/design.md](docs/design.md) explains what that job does.

## Layout

```text
backend/   Go backend (cmd + internal), including the AI assistant
frontend/  Vite single-page app, built to static files
docs/      Design, contributing and code-style documentation
nginx/     Reverse proxy configuration
deploy/    Deployment scripts
docker-compose.yml
```
