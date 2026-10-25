# Contributing to Kada

Thanks for taking the time to contribute. This document covers how to set the project up, how work
is expected to flow, and what a good pull request looks like.

Read [design.md](design.md) first for how the system fits together, and
[code-style.md](code-style.md) for the conventions used in the code itself.

## Contents

- [1. Requirements](#1-requirements)
- [2. Local setup](#2-local-setup)
- [3. Running the stack](#3-running-the-stack)
- [4. Everyday commands](#4-everyday-commands)
- [5. Making a change](#5-making-a-change)
- [6. Commit messages](#6-commit-messages)
- [7. Issues](#7-issues)
- [8. Pull requests](#8-pull-requests)
- [9. Testing expectations](#9-testing-expectations)
- [10. Changing the database schema](#10-changing-the-database-schema)
- [11. Documentation](#11-documentation)

## 1. Requirements

| Tool | Version | Why |
|---|---|---|
| Go | 1.26+ | Backend; the module declares `go 1.26.4` |
| Node.js | 22+ | Frontend |
| Docker + Compose | recent | PostgreSQL, Redis, and the full stack |
| `make` | any | Task shortcuts (optional, the commands work standalone) |

You do not need Docker for backend work if PostgreSQL and Redis are already reachable.

## 2. Local setup

```bash
git clone git@github.com:booondenz/kada.git
cd kada

# Environment for the API and for Docker Compose: one file, read from the repository root
# (JWT_SECRET and POSTGRES_PASSWORD are required)
cp .env.example .env

# Web dependencies
cd web && npm ci && cd ..
```

Fill in `.env` before starting anything:

- `JWT_SECRET` — a strong random value: `openssl rand -hex 32`.
- `POSTGRES_PASSWORD` — anything non-default.
- `ADMIN_SECRET` — optional, and empty is a valid local choice: it refuses every operator route, which
  only costs you the takedown console. In production the deploy job writes it from the repository secret
  with `deploy/upsert-env.sh`, as the first step of the deploy.

### Line endings

The repository pins LF through `.gitattributes`. Keep that as is: `core.autocrlf=true` on Windows
will otherwise check Go files out as CRLF, and every Go formatter will then report the whole tree as
unformatted while CI sees it as clean.

## 3. Running the stack

### Everything in Docker

```bash
docker compose up -d      # nginx :80, PostgreSQL :5432, Redis :6379 (internal)
docker compose logs -f
docker compose down
```

The API applies the schema on startup (`DB_AUTO_MIGRATE=true` in Compose), so a fresh volume comes up
ready to use. Nginx serves the app at `http://localhost`.

### The API and the web app on the host

```bash
docker compose up -d postgres redis
go run ./cmd/migrate/   # apply the schema
go run ./cmd/server/main.go   # API on :8080
cd web && npm run dev         # web app on :3000
```

When the web app runs on the host, keep `VITE_API_URL` pointing at the API
(`http://localhost:8080`) in `web/.env.local`.

## 4. Everyday commands

There is no Makefile: these are the commands CI runs, and a wrapper would only be one more place for the
two to drift apart.

| Task | Command |
|---|---|
| Run the API | `go run ./cmd/server/main.go` |
| Run the web app | `cd web && npm run dev` |
| Go tests | `go test ./... -v` |
| Web tests | `cd web && npm test` |
| Go tests with the race detector (what CI runs) | `go test ./... -v -count=1 -race -coverprofile=coverage.out` |
| `go vet` / ESLint | `go vet ./...` / `cd web && npm run lint` |
| `golangci-lint` (what CI runs) | `golangci-lint run --timeout=5m ./...` |
| Apply the schema | `go run ./cmd/migrate/` |
| Destroy the database volume and start over | `docker compose down -v && docker compose up -d postgres redis && go run ./cmd/migrate/` |
| Production builds | `CGO_ENABLED=0 go build -o bin/server ./cmd/server/main.go` / `cd web && npm run build` |
| Compose lifecycle | `docker compose up -d` / `docker compose down` / `docker compose logs -f` |

Before opening a pull request, these should all pass:

```bash
go vet ./... && go test ./... -count=1 -race
cd web && npm run lint && npx tsc --noEmit && npm run build
```

## 5. Making a change

1. **Open the issue first.** Before the branch, before the code, before deciding the change is small
   enough not to need one. [§7](#7-issues) says what goes in it; the point of writing it first is that it
   is the only step that has to decide what "done" means *before* there is an implementation to argue
   with. A fix whose issue was written afterwards is a fix nobody can find later: the tracker is the only
   list of what was wrong with this project, and the commit log is not searchable by symptom.
2. **Branch off `main`.** Use a short descriptive name: `feat/ai-support`, `fix/sms-signature-error`.
3. **Keep the change focused.** One topic per branch and per pull request. Unrelated cleanups make a
   change hard to review and hard to revert.
4. **Follow the existing layering.** New behaviour belongs in a service; the handler stays thin. See
   [design.md §4](design.md#4-backend).
5. **Keep the layers honest.** Handlers do not run SQL, services do not touch `*gin.Context`,
   ownership is enforced in the `WHERE` clause.
6. **Write the tests that would have caught the bug** or that pin the new behaviour.
7. **Update the documentation** in the same pull request: `docs/design.md` when a decision or a flow
   changes, `README.md` when setup or commands change.

### Adding a backend resource

The pattern is the same for every resource:

```text
internal/domain/models.go         request/response structs (JSON + binding tags)
internal/domain/entity/models.go  the table (only if the schema changes)
internal/service/<res>_service.go business logic
internal/handler/<res>/handler.go thin HTTP layer + RegisterRoutes
cmd/server/main.go                build the service and register the routes
```

Add the rows to `entity.Models()` if you added a table, then run `go run ./cmd/migrate/`.

### Adding UI text

The UI is bilingual and keyed by the Chinese source string:

```tsx
const t = useT();
<button>{t("创建链接")}</button>
```

Add the English entry to `web/src/lib/i18n/dictionary.ts` in the same change. Never hardcode
display text in a component, and never leave a key untranslated.

## 6. Commit messages

Every commit message is **in English**, **one line long**, and starts with a conventional-commit
prefix:

```text
<type>: <short imperative summary>
```

There is no body and no second paragraph. The summary says what changed; the reason belongs where a
reader meets it next to the thing it explains - a comment at the decision, the issue that asked for
the change, or `docs/design.md` when it is a design choice. A message that needs a paragraph to be
understood is a sign the paragraph should live somewhere a reader will find it.

Allowed types:

| Type | Use for |
|---|---|
| `feat` | A new user-visible capability |
| `fix` | A bug fix |
| `refactor` | Behaviour-preserving restructuring |
| `perf` | Performance work |
| `docs` | Documentation only |
| `test` | Tests only |
| `style` | Formatting, no behaviour change |
| `chore` | Tooling, dependencies, maintenance |
| `ci` | CI/CD configuration |
| `build` | Build system or container changes |

Rules:

- **One line.** No body, no bullet list, no second paragraph. `git log --oneline` is the view this
  convention is written for.
- **A closing reference is joined to the summary with a conjunction**, never parked in parentheses:
  `fix: drop the retry that never retried and close #12`. GitHub reads the keyword anywhere in the
  message, and a footer would break the rule above. A parenthesised tail reads as an aside - the
  closing is part of what the commit does, so it is written as part of the sentence.
- **Imperative mood in the summary**: "add link expiry validation", not "added" or "adds".
- **Lower case after the colon**, no trailing period, no emoji.
- **No scope.** `fix: verify the deployment over HTTPS`, not `fix(ci): verify the deployment over
  HTTPS`. The prefix already says what kind of change it is, and the summary names the area, so a scope
  repeats one of them. Every commit in this repository follows that shape.
- **No attribution trailers.** Do not add `Co-Authored-By` for tooling, and do not credit an
  assistant in the message.

Examples taken from the project's own history:

```text
fix: hash SMS codes and fix broken attempt limit
refactor: move the backend from pgx to GORM with AutoMigrate
docs: add README and interview prep doc
ci: make security gates effective (gosec + frontend lint/tsc)
```

## 7. Issues

An issue is a report that a problem exists, not a place to think out loud. Three fields, then stop:

```markdown
## Where

`path/to/file.go` `Symbol` (line N), or the endpoint and the request that triggers it.

## Problem

Two to four sentences. What happens, and why it matters - with the evidence that makes it a fact
rather than an impression: the line number, the query that runs on every request, the count that is
wrong.

## Done when

- The observable condition that says the problem is gone.
- The test that will hold it there.
```

**It is written before the work, not after it.** The reason is not bookkeeping: the issue is the only
document that says what "done" means while the implementation is still an opinion, and the only one written
in terms of the symptom rather than the fix - which is what makes it findable six months later by somebody
who has the symptom and not the commit. An incident found in production gets one too, with the log line in
`## Problem`: that is the evidence, and it is the part that is impossible to reconstruct afterwards.

What does not belong: background, alternatives considered, trade-off analysis, and plans for work that
is not being done now. A design choice that needs that much room belongs in [design.md](design.md); a
rejected option belongs in the pull request that chose the other one.

Write the title in the same shape as a commit subject (`type: imperative summary`), and close the issue
from the commit that fixes it (`... and close #12`, see [§6](#6-commit-messages)) rather than leaving it
open - a long list of open issues reads as an abandoned project.

## 8. Pull requests

A pull request should contain a short summary, and then:

- **What changed and why.** Link the issue it closes, if one exists.
- **How it was verified.** Name the commands you ran.
- **Risk.** Say what could break and what you did about it - for example a schema change, a cache
  invalidation, or a change to an authorization check.
- **Screenshots** for anything visual.

Keep the diff reviewable. If a change needs a large mechanical commit (a rename, a reformat), put it
in its own commit so the meaningful part stays readable.

The CI pipeline must be green before merge: `go vet`, `golangci-lint`, Go tests, ESLint,
`tsc --noEmit`, and the frontend production build.

That green pipeline is also the deploy. The same jobs run again on the push to `main`, and the `deploy`
job they gate - which a pull request never starts - writes the runtime env files from repository
secrets, applies the schema with the uploaded `cmd/migrate` binary, and only then restarts the API and
the frontend. A required secret that is missing stops the deployment before any service on the host is
touched, so the build already running keeps serving.

### Reporting a bug

Include what you did, what you expected, what happened, and the relevant log lines. For backend
issues, the API logs the underlying cause of a failure; paste it rather than only the message the UI
showed.

## 9. Testing expectations

| You changed | Add |
|---|---|
| Business logic or validation | A table-driven unit test in `internal/service` |
| An HTTP handler | A test with a fake service implementation (`internal/handler/*/handler_test.go`) |
| The schema | An assertion in `internal/domain/entity/models_test.go` |
| A dynamic or bulk SQL statement | A dry-run test in `internal/service/db_dryrun_test.go` |
| A pure frontend helper | A Vitest test next to it |
| A shell step in `.github/workflows/ci.yml` | A case in `scripts/verify-deploy-step.test.js` |

The last one exists because a workflow step cannot be run locally, so its mistakes only surface in CI on
a real deploy. `node scripts/verify-deploy-step.test.js` extracts the deploy job's verification steps
straight out of the workflow, runs them against a stubbed `curl`, and checks each outcome - reachable,
connection refused, DNS failure, unhealthy payload. It extracts the bundle-upload step the same way and
drives it against a stubbed `ssh`: one good transfer, two failures then success, a short stream that the
size check has to catch, and a host that refuses. It needs a real `bash`; on Windows that means Git
Bash, which the default sandbox cannot start (it needs a signal pipe), so run it from a normal terminal.

A bug fix without a test that fails before the fix is usually incomplete. Tests must not require a
database: services take `*gorm.DB`, so a `DryRun` session is enough to assert on generated SQL, and
handlers take interfaces so they can be tested with fakes.

### Web scripts

A few checks cannot be expressed as a unit test, so they live in `web/scripts/` and are run from `web/`:

| Command | Checks |
|---|---|
| `node scripts/check-theme-css.mjs` | The theme tokens compile, and brand/status colours are still fixed |
| `node scripts/check-theme-timing.mjs` | The theme is applied before the first frame, so there is no flash |
| `node scripts/capture-theme.mjs` | Screenshots every route in both themes into `web/.theme-shots/` |

The last two drive a running dev server through Edge, so they need both, and on Windows they must run
outside the default sandbox. Run `check-theme-timing.mjs` after touching anything that injects the theme
script: a theme applied after the first frame is a visible flash, and it is not obvious from reading the
code.

## 10. Changing the database schema

The schema is the GORM models in `internal/domain/entity`. There are no SQL migration files.

1. Edit or add the model, with explicit `gorm:"type:..."` tags so column types do not drift.
2. Add the model to `entity.Models()` if it is a new table, in dependency order (referenced tables
   first).
3. Update `internal/domain/entity/models_test.go`: expected table name, expected columns, and any new
   unique index or `ON DELETE` rule.
4. Run `go run ./cmd/migrate/` and verify against a real database. The entity tests assert the model's
   intent; only a database proves the DDL is accepted.

AutoMigrate is additive, so an existing installation picks up new tables, columns, indexes and
constraints without data loss - and anything it cannot express stays in place rather than being
dropped. Constraints worth respecting when you edit a model:

- Declare `ON DELETE` behaviour explicitly. It is real data-safety logic, not decoration: deleting a
  user cascades to their own rows but only detaches links.
- Put `constraint:` on the **association** field (for example `Link.User`), not on the foreign-key
  field, or the generated DDL will disagree with the tag you wrote.
- Set `type:` for anything where the Go type would pick the wrong column type.
- Give every unique rule the repository depends on an explicit unique index.

Things AutoMigrate does **not** do, and that therefore need a hand-written statement in a release
procedure: renaming a column, narrowing a type, dropping a column, and backfilling data. Treat these
as deliberate operations with a backup, not as part of a normal change.

## 11. Documentation

| Document | Owns |
|---|---|
| `README.md` | What the project is, how to run it |
| `README.zh.md` | The same README in Chinese; `README.md` is the source, so the two change together |
| `docs/design.md` | Architecture, data model, flows, decisions, limitations (Chinese: `docs/design.zh.md`) |
| `docs/contributing.md` | This file: workflow, commits, issues, pull requests (Chinese: `docs/contributing.zh.md`) |
| `docs/code-style.md` | Conventions for Go, TypeScript and SQL (Chinese: `docs/code-style.zh.md`) |

Documentation is part of the change, not a follow-up. If you make a decision a future reader would
question, write the reason next to the code (`why`, not `what`) and mention it in `design.md` if it
affects the architecture.
