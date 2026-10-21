# Kada Design

Kada is a short-link management and analytics platform. Users create short links, organise them, and
watch how they are clicked. This document explains how the system is put together and why.

- [1. Product summary](#1-product-summary)
- [2. System overview](#2-system-overview)
- [3. Repository layout](#3-repository-layout)
- [4. Backend](#4-backend)
- [5. Data model](#5-data-model)
- [6. Frontend](#6-frontend)
- [7. Key flows](#7-key-flows)
- [8. Cross-cutting decisions](#8-cross-cutting-decisions)
- [9. Configuration](#9-configuration) 
- [10. Deployment](#10-deployment)
- [11. Testing](#11-testing)
- [12. Known limitations](#12-known-limitations)

## 1. Product summary

| Capability | Notes |
|---|---|
| Authentication | Phone + password behind a graphical challenge at registration, JWT bearer token for the API |
| Links | Create, edit, disable, expire, password protect, batch delete and batch tag |
| Organisation | Folders, tags, workspaces |
| Custom domains | DNS TXT ownership verification before a domain can serve links |
| Analytics | Click totals, daily trend, platform breakdown, visitor list, raw event log |
| Integrations | UTM templates, QR codes, CSV export, personal API tokens |
| AI assistant | Bundled product documentation in the prompt, tools that run as the signed-in user |
| Presentation | Bilingual UI (Chinese / English), light and dark themes |

The primary tension in the design is that **redirection is hot and analytics is cold**. Following a
short link must be fast and must not fail because a database is busy, while counting clicks is
allowed to lag by a second. The architecture is split along that line.

## 2. System overview

```text
                       ┌────────────────────────────────────────────┐
   browser ──────────► │ nginx :80                                  │
                       │  /api/  and  /r/  ──► backend             │
                       │  everything else  ──► frontend            │
                       └───────┬───────────────────────┬────────────┘
                               │                       │
                    ┌──────────▼─────────┐   ┌─────────▼──────────┐
                    │ backend (Go/Gin)   │   │ frontend (static)  │
                    │  :8080             │   │  :3000             │
                    └──┬───────┬─────┬───┘   └────────────────────┘
                       │       │     │
          ┌────────────▼─┐ ┌───▼───┐ │
          │ PostgreSQL   │ │ Redis │ │ publish click
          │ (source of   │ │ cache │ │
          │  truth)      │ │ + rate│ │
          └──────▲───────┘ │ limit │ │
                 │         └───────┘ │
                 │                   ▼
                 │            ┌─────────────┐
                 └────────────│   Kafka     │
                    consume   │  topic:     │
                 ┌────────────│  clicks     │
                 │            └─────────────┘
        ┌────────▼─────────┐
        │ worker (Go)      │
        │ :no http port    │
        └──────────────────┘
```

Components:

| Component | Process | Responsibility |
|---|---|---|
| `cmd/server` | `api` | HTTP API, short-link redirection, publishes click events |
| `cmd/worker` | `kafka-worker` | Consumes the `clicks` topic and persists click rows |
| `cmd/migrate` | one-off | Applies the database schema (GORM AutoMigrate) |
| `web/` | `web` | Vite single-page app, served as static files; talks to the API over HTTP only |
| `postgres` | `postgres` | Source of truth; also the analytics store |
| `redis` | `redis` | Short-code cache and rate-limit counters |
| `kafka` | `kafka` | Durable buffer between request handling and click persistence |
| `nginx` | `nginx` | Single entry point, routes `/api/` and `/r/` to the API |

There is exactly one writer of the database schema (see
[4.4 Schema ownership](#44-schema-ownership)) and no service talks to another service's database.
The frontend never touches PostgreSQL or Redis.

## 3. Repository layout

```text
cmd/server/          API entry point: wiring, routes, graceful shutdown
cmd/worker/          Kafka consumer entry point
cmd/migrate/         schema application
config/              environment parsing (single source of configuration)
internal/
  domain/            transport models (JSON tags, request validation)
    entity/          GORM models == the database schema
  handler/           HTTP layer, one package per resource
  service/           business logic and persistence
  infra/             database, redis, SMS, URL safety, link preview, user-agent
  middleware/        JWT auth, rate limiting, real client IP
  mq/                Kafka publisher and event payloads
db/                  (removed) schema used to live here as SQL migrations
web/
  src/router.tsx     the route table: sign-in, privacy, dashboard/**, r/[code]
  src/pages/         one component per route
  src/components/    shared UI components; ui/ holds the primitives every page uses
  src/lib/           API client, auth storage, i18n, utilities
docs/                this documentation
deploy/              server bootstrap scripts and the env-file writer
  nginx/             reverse proxy configuration
scripts/             maintenance scripts
.cache/              (ignored) tool caches: go-build, go-mod, golangci-lint
```

The Go module is the repository root, so `cmd/`, `config/` and `internal/` are directories at the top
level rather than a module nested under one. The web app is a repository of its own living beside them,
with its own `package.json` and `Dockerfile`.

Because it is, `node_modules` sits inside the Go module's file tree and `./...` walks into it: after a
local `npm ci`, `go test ./...` matches a package under `web/node_modules` (the `flatted` package ships
Go files of its own). `.golangci.yml` excludes `^web/` for exactly that reason, and CI never sees it
because the Go jobs do not run `npm ci`.

`.cache/` is not part of the project either: it is where tooling that may not write outside the workspace
puts Go's build cache, module cache and linter cache. It is ignored, nothing here reads it, and deleting it
costs a cold rebuild and a re-download.

The host layout is not the repository layout. The API runs from `/opt/kada/backend` (its binaries, its
`.env` and its backups) and the served bundle lives in `/opt/kada/frontend`, which is bind-mounted into
the nginx container; renaming either means recreating that container, so the two names are allowed to
differ. The deploy job is what maps one onto the other (see [10. Deployment](#10-deployment)).

## 4. Backend

### 4.1 Layering

```text
HTTP request
   │
   ▼
middleware        JWT validation, API-token validation, rate limiting, client IP
   │
   ▼
handler/<res>     bind + validate JSON, map service errors to HTTP status
   │  depends on a small interface it declares itself, so it can be tested with a fake
   ▼
service/<res>     business rules, ownership checks, transactions, cache invalidation
   │  depends on *gorm.DB
   ▼
domain/entity     database rows
```

Rules that keep this honest:

- A handler never touches the database. It declares the slice of the service it needs as an
  interface (`handler/auth.AuthService`), which is why handler tests need no database.
- A service never touches `*gin.Context`. It takes `context.Context` plus plain values.
- Ownership is enforced in the service by putting `user_id` into the `WHERE` clause, never by
  trusting an ID that arrived from the client.
- Errors returned by services are safe to show to a user. Internal details stay in the logs.

### 4.2 Request path

`cmd/server/main.go` is the only place where the object graph is built. It creates the database
handle, Redis client, SMS sender, cache service, Kafka publisher, then every service, then every
handler, and finally registers routes:

```go
v1 := r.Group("/api")
authH.RegisterRoutes(v1, authMW, strictMW)
linkH.RegisterRoutes(v1, authMW)
// ...
```

Each handler owns its own route table (`RegisterRoutes`), so adding a resource means adding one
package and one line in `main.go`, not editing a central route list.

Public endpoints that do not require a session (`/api/auth/*`, `/r/:code`) are registered inside the
handler without the auth middleware; everything else is grouped behind it.

The Python AI service is the one exception, and it is not registered here at all: `/api/ai/*` is a
reverse proxy (`internal/handler/ai`) that authenticates the caller, injects the user id, and forwards
to the AI service with its own mount prefix stripped (`/api/ai/conversations/current` ->
`/conversations/current`). The Python service owns its own paths and knows nothing about `/api/ai`, so
the two surfaces can move independently - and, as everywhere else in this project, no `/v1` appears in a
URL: the `v1` in the snippet above is a variable name.

### 4.3 Short-link resolution

Redirection has its own hot path that deliberately avoids PostgreSQL:

```text
GET /r/:code
   │
   ├─ Redis cache hit ──► serve
   │
   ├─ Redis "missing" hit ──► 404
   │
   └─ miss ──► SELECT ... WHERE short_code = $1 AND is_active
                 │
                 ├─ expired?      ──► error page
                 ├─ has password? ──► password page / verify
                 ├─ platform needs a guide page? ──► intermediate page with deeplinks
                 └─ otherwise     ──► 302 to the target
```

The result is written back to Redis with a TTL, and every mutation (update, delete, batch delete,
workspace delete) invalidates the affected short codes. This is a cache-aside pattern; PostgreSQL
remains the source of truth and a cold cache only costs latency.

A lookup that finds no row is cached as well, under a key of its own and for sixty seconds rather than
ten minutes. Without it a scanner walking short codes turns every request into a query, and a deleted
link leaves the same trail behind. Only a row that is genuinely absent is remembered as absent: an
unreachable database must not write "this code does not exist" for every code that was asked for,
because that verdict would outlive the outage by its whole TTL. Creating or renaming a link clears the
entry, and concurrent misses on one code share a single database read instead of arriving together.

### 4.4 Schema ownership

The schema is defined by the GORM models in `internal/domain/entity` and applied with `AutoMigrate`; the
migration SQL files are gone. `cmd/migrate` applies the models (`go run ./cmd/migrate/`), and
the API server applies them on startup **only** when `DB_AUTO_MIGRATE=true`, so in production a restart
cannot change the database as a side effect.

AutoMigrate is additive: it creates missing tables, columns, indexes and constraints, and never drops
anything. That makes it safe on an existing installation, and it means a rollback to an older binary leaves
the newer columns in place. It also means the models are **not** the whole truth about a database that
predates them: whatever the original raw-SQL schema declared and the models no longer mention stays exactly
as it was, constraints included. Two rounds of that have cost real time:

- A UNIQUE constraint under PostgreSQL's generated name (`users_phone_key`) aborted the whole migration,
  because AutoMigrate tried to drop a constraint derived from the model that did not exist yet. Hence
  `ReconcileLegacyConstraints`.
- `sms_codes.code` was declared NOT NULL back when codes were stored as text. The service stores only
  `sha256(code)` and never writes that column, so the leftover rule rejected every insert with
  `SQLSTATE 23502` - and that insert runs *after* the SMS has been sent. The recipient got a code, the form
  answered "failed to store verification code", and because no row was written the per-phone cooldown could
  never match one, so every retry reached the provider until it answered with a rate limit. Hence
  `ReconcileLegacyColumns`.

Both run inside `infra.Migrate` before `AutoMigrate`, both are written as idempotent DDL, and neither
touches a table that does not exist, so a fresh database is unaffected. The old golang-migrate bookkeeping
table `schema_migrations` may still sit in a database created before the switch; nothing reads or writes it.
The lesson the code keeps: an existing database is a second source of truth, and the only way to reconcile
it is explicitly.

Raw SQL is still used where a single statement is the point — the atomic verification-code consume,
the idempotent click insert, bulk insert-selects — and each of those sites is commented as such.

### 4.5 Error mapping

| Situation | HTTP |
|---|---|
| Invalid or missing body | 400 |
| No or invalid token | 401 |
| Code or password rejected | 401 |
| Not the owner / duplicate resource | 403 / 409 |
| Unknown resource | 404 |
| Rate limited | 429 |
| Anything unexpected | 500, with a generic message |

Services return sentinel errors for the cases a handler must distinguish (for example
`domain.ErrEmailTaken`), and wrap everything else with context so the log explains the failure while
the response does not.

## 5. Data model

```text
users ──┬── links ──┬── click_logs
        │           └── link_tags ── tags
        ├── folders ──┘ (links.folder_id)
        ├── workspaces ─┘ (links.workspace_id)
        ├── domains
        ├── utm_templates
        └── api_tokens

sms_codes       (standalone, keyed by phone)
login_captchas  (standalone, keyed by id)
```

| Table | Purpose | Notable columns |
|---|---|---|
| `users` | Accounts | `phone` (unique, and the whole profile); `email`, `name`, `avatar`, `password_hash`, `wechat_openid` are retained legacy columns nothing reads |
| `links` | Short links | `short_code` (unique), `original_url`, `domain`, `password_hash`, `expires_at`, `is_active`, `click_count` |
| `click_logs` | One row per click request | `kind` (what the row is evidence of, and the only thing the counters read), `action` (which guide page interaction, NULL otherwise), `platform`, `ip`, `referer`, `event_id` (unique, idempotency) |
| `folders`, `tags`, `link_tags` | Organisation | `link_tags` is a plain junction table |
| `workspaces` | Team/project grouping | `slug` (unique), `links.workspace_id` |
| `domains` | Custom domains | `verified`, unique per `(user_id, name)` |
| `utm_templates` | Reusable UTM sets | `utm_*` columns |
| `api_tokens` | Programmatic access | `token_hash` (unique); the raw token is shown once |
| `sms_codes` | Pending SMS logins | `code_hash` (sha256), `ip` (per-IP quotas), `attempts`, `expires_at`; the plaintext code is never stored |
| `login_captchas` | Pending graphical challenges | `code_hash` (sha256 of the upper-cased answer), `attempts`, `expires_at` |

Design rules:

- **Passwords are never stored in a form that can be replayed.** Account and link passwords use
  bcrypt; SMS codes, captchas and API tokens are stored as SHA-256 hashes, and the plaintext of a
  one-time secret is never written to the database or the log.
- **The graphical challenge lives in PostgreSQL, not Redis.** Redis is optional in this deployment - the
  rate limiter fails open when it is down - and a captcha that silently stops being enforced is worse than
  no captcha, because the endpoint still looks protected. The database is not optional.
- **Deletion semantics are explicit.** Deleting a user cascades to their folders, tags, domains,
  templates and tokens, but only detaches links (`ON DELETE SET NULL`) so public links do not vanish.
  Deleting a folder or workspace detaches its links. Deleting a link cascades to its click logs.
- **Timestamps are UTC** (`timestamptz`); the frontend formats for display.

## 6. Frontend

A Vite single-page app built to static files, with React Router for the routes. The dashboard is highly
interactive, so there was never a server-rendered page to gain from; removing the Node process removed a
service, its systemd unit and a whole deployment step with it.

| Path | Content |
|---|---|
| `/` | Redirects to `/dashboard` |
| `/login`, `/register` | Redirects to `/dashboard`: there is no sign-in page (see [6.4](#64-there-is-no-sign-in-page)) |
| `/privacy` | Privacy policy, linked from the consent checkbox before sign-in |
| `/dashboard` | Overview: totals, trend, recent links |
| `/dashboard/links`, `/new`, `/[id]` | Link list, creation form, detail/edit |
| `/dashboard/analytics`, `/events`, `/customers` | Click analytics, raw events, visitors |
| `/dashboard/folders`, `/tags`, `/domains`, `/utm`, `/settings` | Organisation and account settings |
| `/r/[code]` | Client-side fallback page for a short code |

Data fetching uses SWR against `src/lib/api.ts`, which is the single place where the API surface is
declared and where the bearer token is attached. Components never call `fetch` directly.

State that must survive a reload (session token, profile, locale) lives in `localStorage` behind
helpers in `src/lib/auth.ts` and `src/lib/i18n`.

The ICP filing number sits in the app shell's sidebar, next to the link to `/privacy`, because there is no
page a visitor reaches without a session any more: the app opens on the dashboard with a guest session (see
[6.4](#64-there-is-no-sign-in-page)) and a spot check lands there. `/privacy` carries it too. The number is
one constant in `web/src/components/IcpNotice.tsx` and not a translated string: the filing office checks the
characters, and a second copy of them could only ever disagree with the first.

### 6.1 Internationalisation

The UI ships in Chinese and English. The dictionary is keyed by the **Chinese source string**:

```tsx
const t = useT();
return <button>{t("创建链接")}</button>;
```

- `zh` is the identity map, so an untranslated key degrades to readable Chinese rather than a blank.
- `en` holds the translation; a missing key falls back to the key itself.
- The active locale is stored under `kada.locale`. `useT()` returns a stable function, and locale
  changes re-render through the provider.
- Server-rendered output always starts in `zh` because `localStorage` is unreadable during SSR; the
  provider switches after hydration.

When adding UI text: write the Chinese string, wrap it in `t()`, and add the English entry to
`dictionary.ts`. The dictionary is the only place English copy lives.

### 6.2 Theme

The app follows the browser's `prefers-color-scheme` by default and lets the user override it from
Settings → 外观, with a third option that hands control back to the system.

- `data-theme="light|dark"` on `<html>` is the single source of truth. Tailwind's `dark:` variant is
  redefined as an attribute selector, so it works on any browser and the OS setting and an explicit
  choice take the same path.
- **Colours are roles, not shades.** `text-body` and `bg-canvas` resolve per theme; `text-gray-700`
  cannot, because a shade that reads correctly on white is usually wrong on near-black. The scales are
  not inverted wholesale: brand and status colours are fixed, because `bg-indigo-600 text-white` is a
  pair and brightening the indigo for a dark canvas would leave white text on a light blue.
- **A tint is a surface role and both halves move together.** `bg-brand-soft text-brand-ink` is a chip, a
  selected nav item, an icon tile - not a fixed pair - so it is redefined per theme as a unit. Theming
  only one of the two is how a light chip ends up carrying light ink.
- **`gray-50` is the one raw Tailwind neutral remapped for dark mode.** The app uses it purely as a
  neutral surface (`bg-gray-50`, `bg-gray-50/50`, `border-gray-50`, `hover:bg-gray-50`), always under
  `text-*` classes that are themselves themed; left alone it paints white panels and near-white code
  chips on a near-black page. `web/scripts/check-theme-css.mjs` asserts both this and the fact that
  `indigo-600`/`red-600` are *not* remapped, so the two rules cannot be confused by accident.
- **The switch is in the top bar.** A one-click sun/moon toggle sits beside the language switcher and pins
  an explicit choice; Settings keeps the three-way control (follow system / light / dark). Before
  hydration the toggle renders the server's assumption and is disabled, because the real value comes from
  `localStorage` and reading it during the first client render is the hydration mismatch `useHydrated`
  exists to prevent.
- **Links and errors are tokens too.** `text-link` and `text-danger` carry the two roles the neutral ramp
  cannot express, and both are redefined per theme: indigo-700 and red-600 are legible on white and
  vanish on the near-black canvas, indigo-300 and red-400 the other way round. A component writes the
  role and stops thinking about which theme is on.
- An inline `<script>` in the document head sets the attribute while the HTML is parsed. It is
  deliberately **not** `next/script`'s `beforeInteractive`: that queues the body through Next's loader,
  which measured 62ms *after* the first frame and produced a visible light flash. `scripts/check-theme-timing.mjs`
  measures this and fails if the theme lands after the first frame.

Client components that read `localStorage` gate on `useHydrated()` (`src/lib/useHydrated.ts`). Reading a
token or a stored preference straight into render output makes the server's HTML and the client's first
render disagree, and React discards the tree as a hydration mismatch.

### 6.3 A restrained visual language

There was no rule about how the interface should look, so every page decided for itself: the landing
page had a three.js starfield behind a glowing wordmark, the dashboard was a card grid with four tinted
stat tiles, and the two shared nothing but a colour. What the pages are held to now:

- **One accent.** `brand` means the primary action and the current selection, and nothing else. A status
  colour appears when it reports a status; it never decorates. A page whose hierarchy needs four colours
  is a page whose hierarchy is being carried by something other than size and weight.
- **Hairlines, not shadows.** A region is separated by a 1px `line` border and a step of background.
  Elevation is reserved for things that genuinely float (menus, dialogs): a shadow that separates two
  light surfaces does nothing on the near-black canvas, so it has to be reinvented per theme.
- **One radius**, 6px (`rounded-md`). A pill is a pill only where the control really is one.
- **A fixed type scale** - 12 / 14 / 16 / 20 / 30-36, weights 400/500/600. No `font-black`, no
  `tracking-[0.5em]`: a heading is told apart by size, not by an effect applied to one.
- **Fixed heights on controls** (`h-8`, `h-9`) rather than `py-*`, so a row of them lines up.
- **Motion only for state**: a colour or a transform that says what just happened, over 150-200ms. No
  parallax, no bars that grow on arrival, nothing that runs on its own.
- **One place per control.** `web/src/components/ui/` holds `Button`, `Card`, `PageHeader` and `Field`;
  twelve dashboard pages each writing their own card is how the drift started. `ui/ui.test.tsx` asserts
  the rules above - no shadow on a card, the one radius, heights rather than padding - so a later "make
  this pop" has to break a test to land.
- **There is no landing page and no sign-in page.** `/` redirects to the dashboard, whose shell opens a
  guest session and renders the app (see [6.4](#64-there-is-no-sign-in-page)). Neither page was free:
  `router.tsx` imports every page statically, so the three.js scene that used to sit behind the front page
  was in the **initial** bundle of every route, and a visitor to `/login` downloaded a 3D engine to look at
  a form. Removing them removed the engine, two routes, and the screens that existed to describe the
  product instead of being it.

The dashboard pages are being moved onto these primitives one at a time; until a page is moved it still
carries its own markup, which is the drift the list above exists to end.

### 6.4 There is no sign-in page

Opening the app is enough to use it. The first page load calls `POST /api/auth/guest`, which writes an
ordinary `users` row with every identity column NULL and returns the same kind of JWT a sign-in would have.
The front end keeps it in `localStorage` behind `ensureSession()`, and every request after that is
authenticated exactly as before - no page had to learn that its user has not signed up. The endpoint sits
under the strict limiter, because each call is a row and it is the cheapest thing here to abuse.

- **A guest is a real account, not a demo mode.** `phone`, `password_hash` and the OAuth columns are all
  nullable and unique, and PostgreSQL allows any number of NULLs in a unique index, so "no identity yet"
  is a state the schema already had. The links a visitor creates have an owner, and that owner can be
  *claimed* later by writing a phone number onto the same row - which is what the payment flow will need,
  since a top-up has to be about an account.
- **One request in flight.** Two components opening a session at once would create two accounts and keep
  whichever token was written last, so `ensureSession()` shares one promise per page.
- **An expired token is replaced, not reused.** The client reads the token's own `exp` claim - unsigned,
  and used only to decide whether to reuse it, never to authorise anything - and opens a new session once
  it has passed. Without that, the app would send a dead token on every request and 401 on every page with
  no sign-in form to recover through.
- **A session that cannot be opened is said out loud.** The shell prints the reason to the console and puts
  a retry button on the screen; a failed first request is otherwise a blank page.
- **Nothing signs out.** There is nothing to sign out of, so the sign-out card went with the sign-in page.
  It comes back with the claim step that would need it.

What this costs, stated plainly: the account lives in one browser's `localStorage`. Clearing site data
loses it, another device is another account, and an expiring token starts a new one. That is the price of
not asking for a phone number first, and the claim step is what turns the account into one that survives.
The phone flows in [7.1](#71-signing-in-is-phone-plus-password) are still registered, still tested, and
still the only way into an account that has one - they are just not the door any more.

## 7. Key flows

### 7.1 Signing in is phone plus password

An account is a phone number and a password. The number is verified once, at registration, with an SMS
code; from then on the password is what signs in. The split exists for one reason: an SMS costs money and
an account is signed into far more often than it is created, so a message is spent to prove the number
belongs to the caller rather than on every visit.

None of this is the way in any more: the app opens as a guest ([6.4](#64-there-is-no-sign-in-page)) and
this is the identity a guest account can be given. It is written down here because the routes are what the
claim step will speak, and because the password bound below is a security decision that does not depend on
which screen calls it.

Two other sign-in paths were removed rather than hidden, and the reason is worth recording:

- **WeChat.** The schema had carried `wechat_openid`/`wechat_unionid` from the start and the config had
  `WECHAT_APP_ID`/`WECHAT_APP_SECRET`, but no route ever read them. Implementing it needs an approved
  WeChat Open Platform application, and an individual cannot register one, so the columns stay (existing
  data is untouched) and nothing else does.
- **Email + password.** It duplicated what the phone path already did, and the phone number is the
  identifier an SMS can actually verify - an email address cannot be proved to belong to anyone.
  `users.email` remains in the schema so old rows keep their data; the API publishes neither it nor the
  password hash, and the route that used to write a name or an email onto the profile (`PATCH /api/me`)
  went with it.

The code path (`login-by-phone`) is kept, and it is not a convenience: an account created before this flow
existed has no password, so a code is the only way into it - and registering the same number with a code is
how such an account gets one. It is also the way back in for a password that has been forgotten, since a
password can be reset only by someone who can receive the message. The claim step will reach it by a small
link rather than presenting it as an equal choice.

### 7.2 Registering, and signing in

```text
GET /api/auth/captcha
  → generate a 4-character code and its SVG (internal/infra/captcha, no image or font dependency)
  → store sha256(NORMALISED code) in login_captchas with a 5-minute expiry
  → 200 { captcha_id, image: "data:image/svg+xml;base64,..." }   (Cache-Control: no-store)

POST /api/auth/send-sms-code   { phone, captcha_id, captcha_code }
  → one UPDATE consumes the challenge atomically (unused, unexpired, attempts < 5)
      failure → 400, and attempts is incremented for that id
  → per-phone quotas: 60s cooldown, 10/day            → 429 + Retry-After
  → per-IP quotas: 10/hour, 30/day                    → 429
  → provider sends the code, sha256(code) is stored with a 5-minute expiry
      provider error → the provider code and message are surfaced (see 8.4)
  → 200

POST /api/auth/register   { phone, code, password }
  → phone format and password length are checked first (8 characters, 72 bytes)
  → one UPDATE consumes the pending code atomically (unused, unexpired, attempts < 5)
      failure → increment attempts for the phone, return 400
  → no account for the number      → create it with bcrypt(password)
  → an account with no password    → set the password (the way off the code path)
  → an account with a password     → 409, refused rather than overwritten
  → sign JWT → 200 { token, user }

POST /api/auth/login   { phone, password }
  → bcrypt compare → 401 on any mismatch, identical for an unknown number and a wrong password
  → sign JWT → 200 { token, user }

POST /api/auth/login-by-phone   { phone, code }
  → the same code consumption, then 401 if no account has that number
  → sign JWT → 200 { token, user }
```

Four properties of that ordering are deliberate:

1. **The captcha is checked first and is burned either way.** An unauthenticated endpoint that makes the
   server send paid messages is the most abusable thing in this app, so no send can happen without a human
   having solved a challenge - and a challenge that survives a refused send is a challenge an automated
   caller can reuse. The client re-fetches one after every attempt for exactly that reason.
2. **The quotas are independent.** The per-phone pair bounds the damage to one victim; the per-IP pair is
   what notices a script walking a list of numbers, which the per-phone quotas never would (each number is
   used once). The per-IP figures are looser on purpose: a campus or office NAT legitimately shares an
   address.
3. **A quota is a 429, a wrong captcha is a 400.** Returning 500 for both - which the handler used to do -
   tells a client to retry a request that will keep failing, and tells an operator to look for a server
   fault that does not exist.
4. **A password sign-in cannot be asked which way it failed.** An unknown number, a wrong password and an
   account with no password all answer the same 401 with the same message, and the unknown number is still
   verified against a throwaway bcrypt hash so the three take the same time. Otherwise the endpoint answers
   "does this number have an account here" - to anyone, at twenty guesses a minute per address, which is
   what the strict limiter on these routes allows.

The password is hashed with bcrypt, and a hash stored by the pre-Go backend is still accepted and upgraded
on the next successful sign-in. Both hashes live in `users.password_hash`, so this change needed no
migration: nothing had ever written that column, which is also why "register" is the endpoint that fills it
in for accounts that already exist.

### 7.3 Recording a click

```text
GET /r/:code
  → resolve the link (cache, then database)
  → build a click event with a random event_id and a kind
  → publish to Kafka
      publish fails → write the click row directly (degraded mode, same event_id)
  → redirect the visitor, or serve the guide page (WeChat, QQ, Xiaohongshu)

worker: FetchMessage → WriteClick → CommitMessages
  INSERT click_logs ... ON CONFLICT (event_id) DO NOTHING
      inserted, kind = visit → UPDATE links SET click_count = click_count + 1
      inserted, any other kind → commit only (recorded, not counted)
      conflict → commit only (the click was already counted)
```

Kafka is at-least-once, so `event_id` is what makes counting exact. The consumer commits offsets
only after a successful write, and a poison message (invalid JSON, foreign-key violation) is dropped
after three attempts rather than blocking the single-partition consumer group forever.

Recording a click happens on a goroutine of its own: the redirect handler must not wait for Kafka before
answering, so it spawns the work and returns. That has a consequence the code has to be written around -
Gin's recovery covers only the goroutine that serves the request, so **anything that panics here ends the
process**. The publish call is therefore wrapped so that a panic becomes an error and lands in the same
direct-write fallback as a returned error.

This is not defensive programming for its own sake. With no broker configured, `NewKafkaClickPublisher`
returns a nil `*KafkaClickPublisher`, and the API used to store that in the `mq.ClickPublisher` interface -
where it is no longer nil, so the service's own `kafka != nil` guard passed and every click called a method
on a nil receiver. The API died on the first click it served, systemd restarted it, nginx answered 502 for
whatever arrived in the seconds between, and the click was lost rather than written directly. Two things
prevent it now: `cmd/server` builds the publisher into an interface variable that stays nil when Kafka is
off, and `PublishClick` reports an error instead of dereferencing itself.

#### What counts as a visit

A request to a short link is not evidence that a person opened it. WeChat and QQ fetch a link to build
the preview card before anyone taps it, and that fetcher sends the in-app browser's own User-Agent - so
a User-Agent filter cannot separate them, and any pattern aggressive enough to catch the fetcher throws
away the visitors with it. Every click row therefore carries a `kind`:

| `kind` | What it is evidence of | Counted |
|---|---|---|
| `visit` | A person opened the link | yes |
| `request` | A hit that is not evidence of a person | no |
| `action` | An interaction on the guide page after the visit | no |

Two things produce a visit. A client sent straight to the target never runs any of our JavaScript, so
its request is the only evidence there will ever be - unless its User-Agent names a crawler, which is a
claim a browser never makes (`ua.IsCrawler`, a short list of tokens no browser sends). A client served
the guide page instead is recorded as a `request`, and the page confirms itself with
`POST /r/:code/visit` once its scripts run; a prefetcher that fetched the same page never gets that far.

Only `visit` rows move a counter, and every aggregate - `links.click_count`, the platform breakdown, the
daily trend, the visitor list - filters on it. The other rows stay in `click_logs`, where the amount of
prefetch is still measurable (`/api/analytics/events?kind=request`).

Actions are recorded but never counted: the visit was confirmed when the page loaded, so counting the
buttons a person taps would turn one visit into several. Each one carries its name in the `action` column,
validated against the closed set in `domain.AllActions` - the four interactions the guide page can report.
An unrecognised action is a 400, not a stored string.

The action used to be stored as `action:<name>` in `referer`, so a report that read referers had to know
that some of its rows were not referers at all, and any client could write whatever it liked into that
column. The rows recorded before the column existed are moved across by the migration
(`schema.ReconcileLegacyActions`); their `kind` is left alone, because they moved a denormalised counter
when they were written and that number cannot be recomputed from the rows.

Neither the action endpoint nor the visit confirmation stores a referer. Both are POSTs from the guide page
this backend served, so the `Referer` header on them is our own URL: storing it would put our own domain at
the top of every referrer report. Where a visitor came from is recorded on the request row instead.

The column was added with a default of `visit` because of the rows that already existed: they were
recorded before it did, and they are already counted in `click_count` and in every chart, so `visit` is
the value that leaves that history alone.

### 7.4 What the redirect path reports

`GET /metrics` serves the Prometheus text format: the redirect handler's outcomes and latency, what the
short-code cache did with every lookup, what happened to every click on its way to the store, and the Go
runtime's own series. The vocabulary - names and label values - lives in `internal/metrics` rather than at
the call sites, because a mistyped label value does not fail: it creates a series that never appears, and
the only way to notice is to go looking for a graph that is not there.

Two details of the instrumentation are load-bearing:

- **The outcome is named by whichever branch answers**, and recorded once by a deferred call that also
  times the handler. A branch can therefore forget to name itself, but it cannot record a count without a
  latency: the two would otherwise drift apart silently. A branch that forgets leaves its request in
  `outcome="unknown"`, which is a series that should never exist.
- **The buckets start below a millisecond.** A cache-warm redirect is hundreds of microseconds, so a
  ladder whose smallest bucket is 10ms would put almost every observation in the first bucket and answer
  nothing about the path this exists to measure.

The hit ratio this exposes is `(hit + negative_hit) / (hit + negative_hit + miss)`: both of the first two
are answers served without PostgreSQL, and `miss` is a lookup that reached it. `negative_stored` is the
write that turns a stream of requests for an absent code into requests the database never sees.

`/metrics` is deliberately not reachable from the internet. nginx proxies `/api/`, `/r/` and `/assets/`
and serves everything else from the frontend build, so a request for `/metrics` from outside gets
`index.html`; reaching it means being on the host or holding a tunnel. These series describe traffic and
cache behaviour, so publishing them is a decision that wants a token or an allowlist in front of it
rather than a wider bind.

The cost is a dependency: `prometheus/client_golang` and its tree, which also moved `x/net` and
`google.golang.org/protobuf` forward. Hand-rolling the exposition was rejected because the format is an
interface other software parses, and because the runtime series (goroutines, GC, resident memory) come
with it and are half of what a baseline is.

### 7.5 What the numbers turned out to be

`cmd/loadgen` seeds its own links, drives the two scenarios, and reads the counters above before and after
each one: a hit cannot be inferred from the client - a 302 looks the same either way - and the issue asked
for the ratio rather than for a claim about the code. Every request carries an `X-Real-IP` from a pool,
because the limiter keys on the client address: without that, a run from one machine would be answered with
429 after 120 requests and would have measured the limiter.

The first measurement answered a question the code could not. At 50 concurrent clients the working set ran
at 1,610 requests a second and the stream of absent codes at 5,105. The difference is the click write: an
absent code records nothing, because there is no link to attach the click to, so that path is Redis and
HTTP; a 302 runs the publisher, and in a stack without Kafka that is the degraded direct write, two
statements in one transaction per click. Raising the working set from 200 to 2,000 codes moved throughput
to 1,776, which rules the cache and the size of the working set out as the constraint and leaves the write
volume - the thing Kafka exists to move off the request path.

## 8. Cross-cutting decisions

### 8.1 Configuration

One `Config` struct in `config` is parsed once from the environment; nothing else reads
`os.Getenv`. Missing values fall back to development defaults, except in release mode where weak JWT
secrets are rejected outright. `.env.example` at the repository root documents every variable, for the
process and for `docker compose` alike: the module used to carry a second copy of it, which meant a
setting could be documented in the half nobody was reading.

### 8.2 Caching and degradation

Redis is used for the short-code cache and for rate-limit counters, and both **fail open**: if Redis
is unreachable the API keeps serving, caching is skipped, and rate limiting stops rejecting requests
until the client reconnects. Availability of the product is ranked above strictness of the limiter.

### 8.3 Rate limiting

Sliding-window counters in Redis, in four tiers: a global limit for normal API traffic, a strict limit for
auth endpoints, a higher-throughput limit for redirection, and a tighter one for the guide page's action
reports. The client IP comes from `X-Real-IP`, which nginx rewrites. `X-Forwarded-For` is not trusted,
because a client can forge it.

The action tier is the only one mounted on a single route rather than on a group, and its ceiling is the
lower one because nothing a visitor sees depends on those reports: they are recorded, never counted, so a
limit that drops one costs a line of engagement detail, while what it bounds is a script manufacturing
activity on somebody else's link. The visit confirmation stays under the redirect limit despite being a
POST - it is what makes a click count, so dropping one loses a real visit - and its volume can only ever be
a fraction of the redirects that served the page it reports from.

### 8.4 Third-party failures are made visible

An SMS provider rejection (unapproved signature, unapproved template, disabled AccessKey) is a
configuration mistake, not a transient failure. The provider's error code and message are logged and
- outside release mode - returned to the caller, because a generic "please try again later" makes the
configuration impossible to diagnose. Internal details are suppressed in release mode.

### 8.5 Security posture

- JWT: HS256 only, expiry required, secret strength enforced in release mode.
- Link targets: only `http` and `https` are accepted, which also blocks `javascript:` and `data:`
  payloads from the guide page.
- Link preview: the fetcher refuses internal networks and cloud metadata addresses (SSRF).
- CSV export: fields are escaped and formula-injection prefixes are neutralised.
- Error responses never echo internal errors.
- Containers run as a non-root user; nginx sets the usual response security headers.
- The AI service refuses its own paths (`/chat`, `/conversations/*`) unless the request carries the
  gateway's shared secret, so being able to reach `127.0.0.1:8000` is not the same as being allowed to
  use it (see 8.6).

### 8.6 AI tools act as the signed-in user

The AI service holds no credential of its own. The Go gateway already forwards the caller's
`Authorization` header, and the tools that read link statistics or create short links pass it on to the
existing `/api/*` routes. Four consequences decide the shape of that:

- **Permission checks live in exactly one place.** The assistant does not parse or validate the JWT and has
  nothing to validate: the handler already knows the user, because the request reached it through the same
  middleware as every other route. A disabled account or an expired token fails a tool call exactly as it
  fails the user's own call, and nothing has to be restarted when that happens.
- **The user id is the authenticated one, and there is no header to spoof.** It used to be a header the
  gateway injected, trustworthy only while the request also carried a shared secret that no other local
  process knew - and that second half is what made forging the header impossible rather than merely
  unlikely. In this process the value comes from the middleware's context, so the whole exchange is gone:
  no header, no secret, and no way for a local process to impersonate a user.
- **The credential is per request, not per process.** The user id travels in the request context, so two
  users served by the same process cannot see each other's data - and the same fact is what lets one
  agent, built once at startup, serve every account. (The Python service kept it in a `ContextVar` bound
  by the chat route; that is the same idea, and the reason its business tools could not live in the MCP
  subprocess, whose single shared stdio session has no request to belong to.)
- **A failed tool degrades the answer, not the request.** Tools return their failure as text (an HTTP
  status, a missing credential) so the model can explain it, in the same spirit as the bundled
  documentation failing to load: the answer gets worse, and the request still finishes.

## 9. Configuration

| Variable | Default | Purpose |
|---|---|---|
| `DATABASE_URL` | local `kada` database | PostgreSQL DSN |
| `DB_AUTO_MIGRATE` | `true` | Apply the schema on API startup |
| `REDIS_URL` | `redis://localhost:6379` | Cache and rate limiting (optional) |
| `JWT_SECRET` | weak dev value | Token signing; must be strong in release mode |
| `JWT_EXPIRES_IN` | `720h` | Token lifetime |
| `PORT` | `8080` | API listen port |
| `GIN_MODE` | `debug` | `release` silences dev output and enables strict checks |
| `API_BASE_URL` | `https://kada.click` | Base for generated short URLs |
| `FRONTEND_URL` | `http://localhost:3000` | CORS and redirects |
| `SMS_ACCESS_KEY_ID`, `SMS_ACCESS_KEY_SECRET` | empty | Alibaba Cloud credentials; empty disables real sending (and, in production, sign-in) |
| `SMS_SIGN_NAME`, `SMS_TEMPLATE_CODE` | empty | The system-granted signature and template from the PNVS console; both are required, see below |
| `KAFKA_BROKERS` | empty | Comma-separated brokers; empty disables Kafka (clicks are written directly) |
| `KAFKA_TOPIC` | `clicks` | Click event topic |
| `VITE_API_URL` | `""` (same origin) | API base for the browser, fixed at build time |
| `DEEPSEEK_API_KEY` | empty | The assistant's chat model key (DeepSeek, OpenAI-compatible API) |
| `AI_CHAT_MODEL`, `DEEPSEEK_BASE_URL`, `AI_MAX_TOKENS` | `deepseek-flash`, `https://api.deepseek.com`, `8192` | Optional overrides for the three values above |

`SMS_SIGN_NAME` has no default on purpose. It used to fall back to the literal `kada`, and for this
deployment that is in fact the account's real signature - which is precisely what made the outage
invisible: with the signature always supplied by a default, the missing template code (see 9.1) still
produced a constructed sender, and the failure came back as an Aliyun rejection rather than as "this is
not configured". Empty now means "not configured", and the startup log says so. The cost is one explicit
setting: `SMS_SIGN_NAME=kada` for this account.

None of these secrets are set on the server by hand any more. The deploy job writes them from GitHub
repository secrets with `deploy/upsert-env.sh`, as its **first** step, before a single service is replaced.

Every one of them is `--require`d, and the check reads **the file**, not the repository secrets. That
placement is the whole design: an AI key or an SMS credential that is missing fails the deployment while
the previous build keeps serving, and it fails before anything on the host has been touched. Reading the
file rather than the secrets is also what keeps a hand-configured host deployable - `--set` with an empty
value leaves an existing line alone, so a value that only exists on the server satisfies the check, and a
check that looked at `secrets.*` instead would reject a deployment that was going to work.

The SMS credentials were briefly exempt from that (`--warn`, i.e. report and continue), on the argument
that the Aliyun signature and template have to be approved in the console before they exist at all and
blocking every deploy until then would stop unrelated fixes from shipping. That argument lost: nobody can
register without them, and an account that has no password can only be reached through a code - so a host
without them serves a site that works for the people already on it and is closed to everyone else. A
deployment that would produce that is better refused. What survives from the episode is a runner-side note
(never a failure) saying that CI does not *manage* these four values yet - useful because whether they are
set here or only on the host is invisible until the machine is rebuilt.

The assistant reads these from the same environment as the rest of the API (see
`.env.example`), and in production the deploy job writes them into `/opt/kada/backend/.env`.
A missing key does not stop the process: without the chat key every question is answered with a
configuration message.

> There is no service token and no second process. The tools act as the signed-in user because they run
> in this process with the user id from the request (see 8.6) - the credential the Python service used to
> carry, the internal secret that authenticated the hop to it, and the second database it kept its
> conversations in are all gone with it.

### 9.1 SMS verification

Sign-up by phone uses Alibaba Cloud **PNVS "SMS verification"** (`dypnsapi.aliyuncs.com`), not the
separate SMS product (`dysmsapi`). That distinction decides the configuration:

- It is the one SMS route open to **individually verified** accounts. The SMS product stopped accepting
  personal self-use qualifications, so personal signatures and templates can no longer be approved there.
- The account gets **one** system-granted signature and **one** system-granted template, read from the
  PNVS console (**短信认证 → 概览**). For this account they are the signature `恒创联众` and the template
  `100001`, whose content is `您的验证码为${code}，…以上验证码${min}分钟内有效…`. They cannot be created or
  edited, and they must be used as a pair: a granted signature with a custom template is rejected, and so
  is the reverse.
- The two products keep **separate registries**, and the same string means nothing in the other one. This
  was settled for this account rather than argued: the SMS product's own `QuerySmsSignList` and
  `QuerySmsTemplateList` both answer `TotalCount: 0`, so no signature and no template of that product
  exists here, while PNVS answers `isv.INVALID_PARAMETERS` / 签名或者模版无效 for anything that is not the
  granted pair.
- `SMS_SIGN_NAME` and `SMS_TEMPLATE_CODE` have **no defaults**. They used to fall back to the granted pair
  hardcoded in `aliyun.go`, which is why sends worked before anything was configured at all - and why an
  unset variable looked harmless. Startup now names the missing setting instead, and phone sign-up stays
  disabled until it is set.
- The API contract is not the obvious one either. `SendSmsVerifyCode` **generates the code itself**: the
  template variable must carry the placeholder `##code##` for Aliyun to substitute, and the generated code
  only comes back because `ReturnVerifyCode` is set. A code of our own in that field is rejected with
  `isv.INVALID_PARAMETERS` before any message is sent. `CodeLength` is pinned to 6 because its default is
  4 while the sign-in form, the stored hash and the input field all expect six.
- `SchemeName` is left out, so the provider uses the default service. It is an account-specific name whose
  documented default is exactly that, and the hardcoded `SMS` the request used to carry was a guess that
  nothing had ever checked - probed against the live account, the granted pair answers `OK` as soon as the
  parameter is absent. `CheckSmsVerifyCode` omits it for the same reason: a check has to name the scheme
  the code was issued under.
- The console describes the granted pair as being for **API 联调** and requires test phone numbers to be
  bound before 联调 starts (up to five), so the granted pair is what makes the flow testable for free;
  sending to arbitrary numbers needs a signature and a template of its own.
- The provider enforces a send interval of its own and reports it as a business code on an HTTP 200
  (`biz.FREQUENCY` here, `isv.BUSINESS_LIMIT_CONTROL` on the SMS product). That is a wait, not a
  misconfiguration, and it is not reported as one: the sender flags it, the service turns it into the same
  quota it uses for its own limits, and the client gets a 429 with a retry-after rather than a 400 carrying
  an English provider code into a Chinese form. The codes are matched on the substrings `FREQUENCY` and
  `LIMIT`, because a fixed list would quietly stop matching the next variant.
- The PNVS console, its data, and its package are all separate from the SMS product: sending is billed
  against a PNVS "SMS verification" package, which the SMS product's free trial does not cover.

**Where the working pair went (worth reading before debugging "SMS stopped working").**

SMS really did work in production once, and the reason it stopped is on the record:

1. `05eda08 feat: add Alibaba Cloud SMS verification` moved the pair out of the source and recorded the
   **wrong** values in `.env.example`: signature `kada` and a template code of the form `SMS_…`.
   Neither exists in either registry of this account. Nothing appeared to break, because the source still
   held the correct pair as a hardcoded fallback and kept using it.
2. `8420bd9 refactor: move the backend from pgx to GORM with AutoMigrate` blanked that whole SMS section of
   the example file while rewriting it, so the only recorded copy of the template code disappeared. Still
   nothing broke, for the same reason.
3. `fbc1763 fix: fail on an unset SMS signature instead of substituting one` removed that fallback, which
   was right on its own terms - a hardcoded account-specific pair has no business being a default. But the
   host had no signature configured, so sending stopped, and the values then copied in by hand came from
   the example file, which is to say the wrong ones. From there every send was rejected as an invalid
   signature or template, and the rejection read like a broken account rather than a wrong pair.
4. What ended it was reading the console instead of the repository: **短信认证 → 概览** lists the granted
   signature and template, and a signature probe against both products showed the recorded values belong to
   neither. The lesson is not "the value was lost" but "a wrong value was recorded as if it were the
   account's, and nothing ever checked it against the provider".

The AccessKey pair is **not in the repository and never was** (it is only ever read from the environment; a
history search for the `LTAI` prefix finds nothing but accidental substrings inside base64 hashes in
`go.sum`). The signature and template code are account-specific and read from the environment too, and
`.env.example` deliberately keeps them empty so a copied example file cannot pretend to be
configured. They are named above because they are not secrets - they are visible in the console - and
because refusing to name them is what turned one wrong assumption into an outage that took a console
screenshot to end.

## 10. Deployment

Three supported shapes:

1. **Docker Compose** (`docker compose up -d`): nginx, API, worker, web, PostgreSQL, Redis and
   Kafka on one host. Suitable for a single server or local development.
2. **systemd + released binaries** (`deploy/`): the API and worker run as native processes, with
   PostgreSQL, Redis and Kafka provided separately. This is what the GitHub Actions deploy job uses:
   it builds the binaries, applies the schema with `bin/migrate`, restarts `kada-api`, and verifies
   the health endpoint, rolling back to the previous binary if the service does not come up.
3. **The assistant inside the API process.** It used to be a Python container of its own with its own
   database, its own env file and its own deploy script; it is a package of the Go binary now
   (`internal/assistant`), which is why none of those exist any more. Its product documentation is
   compiled into the binary and sent with every question, so the chat and tool code all runs in the API
   process - one fewer service to run, one fewer way for two processes to disagree about who is asking,
   and no indexing step in the deploy.

   The provider keys reach the host from **GitHub repository secrets**, written into
   `/opt/kada/backend/.env` by `deploy/upsert-env.sh` as the first step of the deploy, before any service
   is replaced. That file is the fix for a whole class of outage: both "the AI page is broken" and "the
   SMS code never arrives" turned out to be an empty line in a file on the server that nothing had ever
   checked. A `--require`d key that is missing fails the deployment while the previous build is still
   serving, and an empty `--set` leaves an existing hand-configured value alone.

The deploy job applies the schema **before** replacing the binary, so a failed migration leaves the
previous version running.

The bytes reach the host as **one tarball over one ssh stream**, and that transfer is bounded: a
per-attempt timeout, three attempts, and a size check before anything is unpacked. It used to be
`appleboy/scp-action` with six sources, one of them a directory glob, and the step took whatever the
runner-to-host link decided - 26 seconds in one run, 8 minutes in another and 27 minutes in a third, with
the action's own command timeout (10m) not bounding the last one. A stalled transfer is now a failure that
leaves the host untouched instead of a run held open until it happens to succeed, and the step that
follows starts from a complete tree under `/tmp/kada-deploy`. The web build happens **once**: the
`web-build` job already produces the bundle with the deployment's own `VITE_API_URL`, so the
deploy job receives it as an artifact rather than setting up Node and building the same commit again.

The frontend swap happens **inside** `/opt/kada/frontend`, never by replacing that directory. nginx runs
in a container with the directory bind-mounted, and a bind mount keeps the inode it resolved when the
container started: `rm -rf` followed by `mv` left it serving the removed, now empty directory, and the
`try_files` fallback turned that into a 500 on every page while `/api/health` still answered 200 - a
broken site behind a green deploy. The deploy therefore copies the new bundle in and renames only
`index.html` into place, which is atomic and is the only name a visitor can be holding (Vite's asset
names are content hashes and are never reused); files the new build does not contain are deleted after
that switch. `scripts/verify-deploy-step.test.js` runs that block against a stand-in directory and asserts
the inode does not change, which is the property the mount depends on.

The container has three read-only bind mounts into the host, which `docker inspect kada-nginx` shows:

    /opt/kada/nginx-prod.conf  ->  /etc/nginx/nginx.conf   (ro)
    /opt/kada/frontend         ->  /opt/kada/frontend      (ro)
    /etc/letsencrypt           ->  /etc/letsencrypt        (ro)

and it runs with `--network host`, because the configuration proxies to `127.0.0.1:8080`, which means this
host only under host networking. All of it lives in the host's `/opt/kada/docker-compose.yml`: a container
created by hand from `docker run` looks identical until the next `docker compose up -d` discards it, mount
and all.

**The configuration in that first mount is deployed from this repository.** It did not use to be: the file
was created on the host by hand and the copy here was documentation that no deployment could apply, so
editing it and deploying changed nothing - silently, because the `nginx -t` the deploy ran validated the
file that was running and reported it fine. Each part of the fix is load-bearing:

- The file is a member of the deploy bundle, so what the host is given is what the repository holds.
- The new content is written **into** the existing file and never over it. A FILE bind mount keeps the
  inode it resolved when the container started, so `mv`, or `rm` followed by `cp`, would leave nginx
  reading the old one while every check passed - the directory trap above, one level down.
- The previous file is backed up and the difference printed before the new one is applied, so an edit
  someone made on the host shows up in the deploy log instead of being overwritten in silence.
- `nginx -t` gates the reload, and a configuration nginx refuses is restored in place and fails the deploy
  while the site keeps serving the previous one. Nothing here assumes the file that was shipped is the file
  that is running: the deploy compares the hash of what the container reads with the hash of what it
  carried, and the on-host check then asks nginx over the network for the page and for the port-80 redirect
  the configuration is supposed to provide. `scripts/verify-deploy-step.test.js` sources that block against
  a stand-in file and asserts the inode, the gate order and the rollback.

Verification happens in two places, which is deliberate:

- **On the host**, during the deploy step: `curl http://127.0.0.1:8080/api/health`, then the site itself
  through nginx (`--resolve`, so no DNS is involved, and `grep 'id="root"'`), then the port-80 entry point,
  asserted to answer 301 - a behaviour the configuration provides, rather than a file that was merely
  installed. All three are authoritative and all three can fail the deploy, because none depends on
  anything but this host, and none can be failed by a third party. The API check alone is not enough: a frontend that is not
  being served - no bind mount, a stale one, a config still proxying to a dead port - leaves the API
  perfectly healthy while every page is a 500.
- **From the runner**, afterwards: a smoke test of the public origin over HTTPS, its `/api/health`,
  the frontend, and the plain-HTTP entry point (which nginx redirects). It covers DNS, TLS and nginx as
  a visitor sees them, and it is **advisory**: a datacenter IP can be refused by the host's edge (this
  deployment answered `Connection reset by peer` during a TLS handshake while the same URL returned 200
  elsewhere), and gating a release on that would make green builds a matter of luck. Its result is
  reported as a warning and in the run summary, never as a failed deploy.

  The step absorbs a refusal rather than letting `curl` raise it. `continue-on-error` keeps it from
  failing the run, but on its own it still lets a non-zero `curl` surface as an error annotation and a
  red X next to a deployment that succeeded. The step therefore always exits 0, writes what happened to
  `verify-status.txt` and `verify-body.txt`, and the step after it reports those. A refusal is a fact to
  record, not an error to raise.

  Two shell details are load-bearing there, and `scripts/verify-deploy-step.test.js` runs this step
  against a stubbed `curl` to keep them honest: the exit status is read from a plain assignment, because
  `if ! curl` reports the status of the negation (a refusal recorded as `curl 0`) and `local rc=$?` reads
  the status of `local`; and only `/api/health` is asserted to carry a healthy payload, since the
  frontend serves HTML and asserting the same payload on it fails a perfectly healthy site.

The public origin defaults to `https://kada.click`. Override it with the `SITE_URL` **repository
variable** (Settings → Secrets and variables → Actions → Variables) when a deployment serves a
different name. It is a variable rather than a secret because the name is already public in the
certificate transparency logs, the DNS records and `deploy/nginx/nginx-prod.conf` - and because a deployment
step that requires manual setup is a failure mode of its own.

### 10.1 The certificate the site serves

The certificate is Let's Encrypt's, issued from this host into certbot's store rather than into this
repository: `/etc/letsencrypt/live/kada.click/{fullchain.pem,privkey.pem}`, named `kada.click`, covering
`kada.click` and `www.kada.click`. `deploy/nginx/nginx-prod.conf` reads those two paths, so the name has to
survive every renewal - which is what `certbot certonly --cert-name kada.click` is for.

The nginx that serves it runs in a container and reads the host's store through a read-only bind mount
(`/etc/letsencrypt -> /etc/letsencrypt (ro)`), the same arrangement as the frontend directory. That gives
the step people forget its shape: **nginx holds the certificate it read when the process started**, so
renewed files on disk change nothing until the process reloads or restarts.

    sudo docker exec kada-nginx nginx -t && sudo docker exec kada-nginx nginx -s reload

**`standalone` cannot renew this certificate on this host, and never could.** It authenticates by binding
port 80, and `kada-nginx` holds port 80 under `--network host`, so every attempt ends with

    Could not bind TCP port 80 because it is already in use by another process on this system

`certbot.timer` is active and runs twice a day; the certificate that expired on 13 October was July's, which
means every attempt since mid-September failed and the only record of that was a mail to root. Stopping the
container first does free the port, and a restart then serves the new certificate - that is a valid one-off,
and it is what the renewal issue describes.

**What renews it now is webroot.** By the time the filing ended the name resolved again, and the frontend
directory is already nginx's root, so certbot writes its challenge where nginx will serve it:
`certbot certonly --webroot -w /opt/kada/frontend`, and `certbot renew --dry-run` is the check that says the
timer can manage on its own - which is the property the timer never had with an authenticator that wanted a
port somebody else was holding.

One consequence of that location is worth knowing. The deploy moves the frontend bundle into that directory
and then deletes every file in it the bundle did not ship (section 10), so a challenge file that appears while
a deploy is running is removed and that attempt fails. Nothing needs to fix this: the next run retries, an
attempt is only ever spent from the thirty days before expiry, and the 14-day warning below is the backstop
that makes one miss uninteresting.

**DNS-01 is still the authenticator for a period in which the name does not resolve.** The zone is Aliyun's
(`dns23.hichina.com`, `dns24.hichina.com`), and DNS-01 validates through a TXT record: no port, no downtime,
and - the property that mattered while the domain carried no records for an ICP filing - no need for the name
to resolve at all. HTTP-01, webroot and TLS-ALPN-01 all need the name to point here, so they are unavailable
for exactly as long as a filing runs; that is why this filing's window could not be renewed the way it is
renewed now.

**Verify from the visitor's side, not from certbot.** `certbot certificates` reads the files on disk and
says nothing about what is being served; `openssl s_client` against the public name says nothing either,
because a host reaching its own address is at the mercy of hairpin NAT. The authoritative local check sends
SNI to the loopback address:

    echo | openssl s_client -connect 127.0.0.1:443 -servername kada.click 2>/dev/null | openssl x509 -noout -subject -dates

**Nothing else watches the expiry**, which is how it reached a day from it. The deploy's own TLS check
passes an expired certificate by design (it passes `-k`, because it asserts which files are being served,
not that they are trusted), and the runner-side smoke test is advisory and cannot reach the site at all
while the name does not resolve. `.github/workflows/certificate.yml` fills that gap: daily, it reads the
expiry **from the host** over the same SSH connection the deploy uses - authoritative, and independent of
DNS and the edge - and opens an issue when less than 14 days remain, commenting on the one that is open
instead of opening a second. It deliberately does not fail the run: a scheduled job that stays red for weeks
is the alarm people stop reading, and the issue is the durable channel. It does fail when it cannot read the
certificate, because a check that checks nothing must not look like a check that passes.
`scripts/verify-certificate-check.test.js` runs both of its steps against a stubbed `ssh` and `gh` to keep
those rules honest.

## 11. Testing

| Layer | Style |
|---|---|
| HTTP handlers | Table-driven tests with fake service implementations; no database |
| Services | Pure-function tests (validation, short-code generation, CSV escaping) |
| Schema | `internal/domain/entity` asserts table names, columns, unique indexes and delete rules against the GORM schema |
| Query shapes | Dry-run GORM sessions assert the generated SQL for the dynamic and bulk statements |
| Frontend | Vitest for pure helpers (`aiAnswer` parsing, utilities) and the UI primitives, rendered to static markup - no DOM, no jsdom |
| Assistant | A fake model, but the real Eino agent with the real tools and the real bundled documentation: one test drives a model that asks for a tool and asserts which user the tool acted for, another drives a model that never stops asking and asserts the step limit holds |

Run everything with `go test ./... -count=1 -race` and `cd web && npm test`; CI
additionally runs `go vet`, `golangci-lint`,
`tsc --noEmit` and the production build on every pull request. The assistant is tested where it runs
rather than as a container: the failure it has to catch - a model call that only breaks when a question
arrives - is a fake model in `internal/assistant`, not an image that starts.

## 12. Known limitations

These are deliberate trade-offs, not oversights:

- **Analytics are computed on demand.** `click_logs` is queried directly; there is no rollup table.
  Acceptable at the current volume, and the obvious first thing to change if the tables grow.
- **Kafka has one partition.** Click ordering is preserved and the throughput ceiling is a single
  consumer; a single-partition consumer group is also what makes the poison-message logic simple.
- **AutoMigrate cannot express every future change.** Renames, type narrowing and data backfills need
  a hand-written statement; there is no down-migration path.
- **No background job scheduler.** Expiry is evaluated at read time rather than by a sweeper.
- **Single-tenant deployment.** There is no organisation/role model beyond `workspaces`, which group
  links but do not grant access to other users.
- **Rate-limit state is per-Redis-instance**, so a multi-instance deployment needs a shared Redis
  (which is the intended topology).
- **An account starts as a guest and stays one until it is claimed.** Its token lives in one browser's
  storage, so a cleared browser or an expired token means a new account and the old one's links are
  unreachable without the phone number that would identify it - which is exactly the gap the claim step
  fills. Anonymous creation is also an open write path: `POST /api/auth/guest` is behind the strict
  limiter, and there is no per-account quota on the links a guest may create yet.
