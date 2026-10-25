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
| Authentication | A guest session is the account: `POST /api/auth/guest` mints the JWT bearer token the API takes |
| Links | Create, edit, disable, expire, password protect, batch delete and batch tag |
| Organisation | Folders and tags |
| Custom domains | DNS TXT ownership verification before a domain can serve links |
| Analytics | Click totals, daily trend, platform breakdown, visitor list, raw event log |
| Integrations | UTM templates, QR codes, CSV export, personal API tokens |
| Presentation | Bilingual UI (Chinese / English), light and dark themes |

The primary tension in the design is that **redirection is hot and analytics is cold**. Following a
short link must be fast and must not fail because a database is busy, while counting clicks is
allowed to lag by a second. The architecture is split along that line.

## 2. System overview

```text
Browser → nginx :80 ─┬→ /api/, /r/ → backend :8080 ─┬→ PostgreSQL (source of truth)
                     │                              └→ Redis (cache + rate limit)
                     └→ other → frontend :3000
```

Components:

| Component | Process | Responsibility |
|---|---|---|
| `cmd/server` | `api` | HTTP API, short-link redirection, click recording |
| `cmd/migrate` | one-off | Applies the database schema (GORM AutoMigrate) |
| `web/` | `web` | Vite single-page app, served as static files; talks to the API over HTTP only |
| `postgres` | `postgres` | Source of truth; also the analytics store |
| `redis` | `redis` | Short-code cache and rate-limit counters |
| `nginx` | `nginx` | Single entry point, routes `/api/` and `/r/` to the API |

There is exactly one writer of the database schema (see
[4.4 Schema ownership](#44-schema-ownership)) and no service talks to another service's database.
The frontend never touches PostgreSQL or Redis.

## 3. Repository layout

```text
cmd/server/          API entry point: wiring, routes, graceful shutdown
cmd/migrate/         schema application
config/              environment parsing (single source of configuration)
internal/
  domain/            transport models (JSON tags, request validation)
    entity/          GORM models == the database schema
  handler/           HTTP layer, one package per resource
  service/           business logic and persistence
  infra/             database, redis, URL safety, link preview, user-agent, schema, storage
  middleware/        JWT auth, rate limiting, real client IP
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
handle, Redis client, cache service, then every service, then every
handler, and finally registers routes:

```go
v1 := r.Group("/api")
authH.RegisterRoutes(v1, authMW, strictMW, guestMW)
linkH.RegisterRoutes(v1, authMW)
// ...
```

Each handler owns its own route table (`RegisterRoutes`), so adding a resource means adding one
package and one line in `main.go`, not editing a central route list.

Public endpoints that do not require a session (`/api/auth/*`, `/r/:code`) are registered inside the
handler without the auth middleware; everything else is grouped behind it.

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

The result is written back to Redis with a TTL, and every mutation (update, delete, batch delete)
invalidates the affected short codes. This is a cache-aside pattern; PostgreSQL
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
        │           ├── link_tags ── tags
        │           ├── link_blocks
        │           └── abuse_reports ──┘ (links.id, nullable)
        ├── folders ──┘ (links.folder_id)
        ├── domains
        ├── utm_templates
        └── api_tokens

blocked_hosts   (standalone, keyed by host)
```

| Table | Purpose | Notable columns |
|---|---|---|
| `users` | Accounts | `phone` (unique, and the whole profile); `email`, `name`, `avatar`, `password_hash`, `wechat_openid` are retained legacy columns nothing reads |
| `links` | Short links | `short_code` (unique), `original_url`, `domain`, `password_hash`, `expires_at`, `is_active`, `click_count`, `blocked_at` (an operator stopped it) |
| `click_logs` | One row per click request | `kind` (what the row is evidence of, and the only thing the counters read), `action` (which guide page interaction, NULL otherwise), `platform`, `ip`, `referer`, `event_id` (unique, idempotency) |
| `folders`, `tags`, `link_tags` | Organisation | `link_tags` is a plain junction table; `links.folder_id` is the other grouping |
| `domains` | Custom domains | `verified`, unique per `(user_id, name)` |
| `utm_templates` | Reusable UTM sets | `utm_*` columns |
| `api_tokens` | Programmatic access | `token_hash` (unique); the raw token is shown once |
| `blocked_hosts` | Targets that are not served | `host` (unique; matched by suffix, so one row covers every subdomain), `category` (why), `note` |
| `link_blocks` | One takedown of one link | `short_code` (copied, so the row outlives the link), `reason`, `evidence`, `operator` (the phone number that decided), `lifted_at`, `lift_reason`, `lifted_by` |
| `abuse_reports` | Complaints from visitors | `short_code`, `link_id` (nullable - a report about a deleted link is still evidence), `reason`, `detail`, `contact`, `ip` (not published) |
| `link_targets` | One destination of a link's pool | `url`, `weight` (share of the draw; 0 means never), `daily_quota` (0 means unlimited), `enabled`, `label`, `kind` (`url` or `wechat_group`), `expires_at` |
| `link_target_audits` | One replacement of a pool | `operator`, `before` and `after` as JSON arrays |
| `uploads` | Files behind file links | `public_id` (unguessable, in the URL), `filename`, `content_type`, `size`, `sha256`, `storage_path` (relative to `UPLOAD_DIR`) |

Design rules:

- **Secrets are never stored in a form that can be replayed.** A link's access password is bcrypt (a
  legacy SHA-256 hash is still accepted and upgraded on the next correct one); API tokens are stored as
  SHA-256 hashes and the raw value is shown once. The plaintext of a one-time secret never reaches the
  database or the log.
- **Redis is optional; the database is not.** The cache and the rate limiter fail open when Redis is down
  (see [8.2](#82-caching-and-degradation)). There is no such fallback for a link that cannot be read,
  because the answer to one is a 500, not a degraded page.
- **Deletion semantics are explicit.** Deleting a user cascades to their folders, tags, domains,
  templates and tokens, but only detaches links (`ON DELETE SET NULL`) so public links do not vanish.
  Deleting a folder detaches its links. Deleting a link cascades to its click logs.
- **A removed feature is removed from the database too.** AutoMigrate only adds, so `schema.ReconcileLegacyTables`
  drops the tables of features the models no longer declare (`workspaces` is the first), and the column that
  held the foreign key to one is dropped by `schema.ReconcileLegacyColumns` before it.
- **A target can be refused after the link exists.** The blocked list is data rather than a constant, and
  the redirect asks it on every request rather than caching the answer with the link, so a row added today
  stops a link created last year (see [7.6](#76-a-blocked-target-is-refused-and-the-refusal-is-not-cached)).
- **Timestamps are UTC** (`timestamptz`); the frontend formats for display.

## 6. Frontend

A Vite single-page app built to static files, with React Router for the routes. The dashboard is highly
interactive, so there was never a server-rendered page to gain from; removing the Node process removed a
service, its systemd unit and a whole deployment step with it.

| Path | Content |
|---|---|
| `/` | Redirects to `/dashboard` |
| `/login`, `/register` | Redirects to `/dashboard`: there is no sign-in page (see [6.4](#64-there-is-no-sign-in-page)) |
| `/privacy` | Privacy policy, reachable without a session and linked from the user page |
| `/dashboard` | Overview: totals, trend, recent links |
| `/dashboard/links`, `/new`, `/[id]` | Link list, creation form, detail/edit |
| `/dashboard/analytics`, `/events`, `/customers` | Click analytics, raw events, visitors |
| `/dashboard/folders`, `/tags`, `/domains`, `/utm` | Organisation |
| `/dashboard/moderation` | The operator console: visitor reports, and stopping or restoring a link by code |
| `/dashboard/settings` | The user page: identity, appearance, API tokens, and the site's own footer |
| `/r/[code]` | Client-side fallback page for a short code |

Data fetching uses SWR against `src/lib/api.ts`, which is the single place where the API surface is
declared and where the bearer token is attached. Components never call `fetch` directly.

The operator console is the one page whose entry the sidebar does not decide on its own: whether an account
is an operator is a deployment setting (`ADMIN_PHONES`), so the shell asks the cheapest operator route once
and shows the entry only if the answer is not a 403. The page itself still says so if it is opened directly -
being added to or removed from that list should be visible, not guessed at. The entry has a group of its
own at the bottom of the sidebar, because it is not a resource the product offers.

State that must survive a reload (session token, profile, locale) lives in `localStorage` behind
helpers in `src/lib/auth.ts` and `src/lib/i18n`.

The ICP filing number sits at the bottom of the user page (`/dashboard/settings`), under the version
string, next to the link to `/privacy`. It moved there from the sidebar: the filing office's link sat
between two navigation links, and nothing about it is navigation. `/privacy` carries it too. The number is
one constant in `web/src/components/IcpNotice.tsx` and not a translated string: the filing office checks the
characters, and a second copy of them could only ever disagree with the first.

The public security filing number sits beside it, in both places, on the same rule - `豫公网安备41138102000392号`
in `web/src/components/PublicSecurityNotice.tsx`, linking to the bureau's own query page. It is a second
filing from a second office, granted after the first, and it was added as one component rather than as a copy
of the ICP one because the two are checked by different offices and can change independently.

The number being one click inside the app rather than on every screen is the weakest point of this
arrangement, and the filing order is about the bottom of the site rather than the bottom of a page. If the
annual review objects, the answer is to put a single line back under the link list - the page every
visitor lands on - and not to move it around again.

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
用户 → 外观, with a third option that hands control back to the system.

- `data-theme="light|dark"` on `<html>` is the single source of truth. Tailwind's `dark:` variant is
  redefined as an attribute selector, so it works on any browser and the OS setting and an explicit
  choice take the same path.
- **Colours are roles, not shades.** `text-body` and `bg-canvas` resolve per theme; `text-gray-700`
  cannot, because a shade that reads correctly on white is usually wrong on near-black. The scales are not
  inverted wholesale: `bg-indigo-600 text-white` is a fill and its type, and lightening the fill for a dark
  canvas would leave white text on a pale blue button.
- **The dark neutrals are one ramp, and lightness is elevation.** The scale is DeepSeek Harness's
  `neutral-bluish` ramp: the page is `#151517`, a card `#232324`, a chip `#2c2c2e`, a filled state
  `#353638` - each surface a step lighter than the one behind it, so a card rises off the page by fill
  rather than by shadow. It replaced a set of Tailwind greys picked shade by shade, which were navy
  (`#0b1220` page, `#111827` card) and put a blue cast on every neutral next to an indigo accent. Hairlines
  are white at 12% and 16%, which is how they keep one contrast step over all four surfaces.
- **A tint is a surface role and both halves move together.** `bg-brand-soft text-brand-ink` is a chip, a
  selected nav item, an icon tile - not a fixed pair - so it is redefined per theme as a unit. Theming only
  one of the two is how a light chip ends up carrying light ink. The same rule covers the status tints
  (`bg-emerald-50 text-emerald-700`, `bg-amber-50 text-amber-800`, `bg-red-50 text-red-600`): in dark mode
  the `-50` half becomes a 14% wash of its own hue and the ink moves up its scale, because a `#ecfdf5`
  patch on `#151517` is a glowing hole in the page. `web/scripts/check-theme-css.mjs` asserts the pairing
  rule and the fact that brand fills are *not* redefined.
- **`gray-50` is the one raw Tailwind neutral remapped for dark mode.** The app uses it purely as a
  neutral surface (`bg-gray-50`, `border-gray-50`, `hover:bg-gray-50`), always under `text-*` classes that
  are themselves themed; left alone it paints white panels and near-white code chips on a near-black page.
  The half-opacity form (`bg-gray-50/50`) is *not* remappable - Tailwind inlines that value as `oklch` -
  which is why the panels that used it now say `bg-subtle`.
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
- **Icons label controls, not content.** A nav row or a button may carry one, because it is a target you
  aim at; a stat tile, an empty state and a wordmark may not, because there is nothing there to hit and the
  glyph is describing itself. The four tinted chips on 分析's tiles reported no status - four numbers are
  four numbers - and the filled indigo square beside "Kada" was the one accent spent on a name.
- **A count that opens its own breakdown is a control.** The click total on a link card was a paragraph
  inside the card's link, so it opened the page the card opened, with the analysis folded away, and it
  looked like a statistic - nothing said it could be clicked at all. A number that is the entry point to
  the numbers behind it carries a control's affordances (hover, tooltip) and lands on the breakdown
  instead of on the summary one click above it.
- **An empty state is a line of text.** The first page a visitor saw used to be a 64px icon tile, a
  heading, a marketing line and a second create button, centred where a list should be - on a page whose
  toolbar already said `0 条链接` and already carried the action. "Nothing here yet" is one sentence, set
  at the size of a sentence: every list says it, and the link list says two things with the same line,
  because a search that matched nothing and a list with nothing in it are different sentences. The other
  five lists say 还没有文件夹, 还没有标签, 还没有 UTM 模板, 还没有自定义域名 and 开始对话, because the control
  that creates the first item is already on the page above them; the icon tile, the heading and the
  instruction that pointed at that control were all the same sentence said three times. A blank panel is
  the one thing an empty list must not be: it is indistinguishable from a page that is still loading.
- **So is a failed one.** A request that did not arrive is one sentence and a reload, not an emoji in a
  rounded box above a heading and a paragraph. Five list pages carried that block; it took the height of
  the list it was standing in for, to say that the list was not there.
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
  is a state the schema already had. The links a visitor creates have an owner, and that owner is the
  `users` row the token names - which is why the id is minted before the visitor has done anything.
- **One request in flight.** Two components opening a session at once would create two accounts and keep
  whichever token was written last, so `ensureSession()` shares one promise per page.
- **An expired token is replaced, not reused.** The client reads the token's own `exp` claim - unsigned,
  and used only to decide whether to reuse it, never to authorise anything - and opens a new session once
  it has passed. Without that, the app would send a dead token on every request and 401 on every page with
  no sign-in form to recover through.
- **A session that cannot be opened is said out loud.** The shell prints the reason to the console and puts
  a retry button on the screen; a failed first request is otherwise a blank page.
- **Nothing signs out.** There is nothing to sign out of, so the sign-out card went with the sign-in page.
  It comes back with a second account to switch between, which is a question about merging links rather
  than about clearing a cookie.

What this costs, stated plainly: the account lives in one browser's `localStorage`. Clearing site data
loses it, another device is another account, and an expiring token starts a new one. That is the price of
not asking for a phone number first, and there is no form that turns the account into one that survives -
[7.1](#71-the-sign-in-that-used-to-be) is what that would have been.

## 7. Key flows

### 7.1 The sign-in that used to be

An account was a phone number and a password. The number was verified once, at registration, with an SMS
code; from then on the password signed in. The split existed for one reason: an SMS costs money, and an
account is opened far more often than it is created, so the message was spent proving the number rather
than on every visit.

None of it is the way in any more (see [6.4](#64-there-is-no-sign-in-page)). What is worth recording is
what removing it left behind:

- **The routes are gone, not hidden.** `internal/handler/auth` registers `POST /auth/guest` and `GET /me`.
  The captcha, the SMS send, register, login and login-by-phone were deleted with the screens that called
  them, so `/api/auth/login` answers 404: there is no handler left to refuse it.
- **The columns stayed.** `users.phone`, `.email`, `.password_hash` and the two WeChat columns are still in
  the model and in the database (see [5](#5-data-model)). AutoMigrate adds and never drops, so deleting the
  fields would leave the columns behind anyway; nothing reads them and nothing writes them.
- **The tables did not.** `sms_codes`, `login_captchas` and `login_attempts` are dropped by
  `ReconcileLegacyTables` on the next migration. A table for a feature that no longer exists leaves open
  the question of which of the two is the schema, and the models are the answer.
- **`last_login_at` still moves**, because opening a session is the event it names.

### 7.2 Opening a session

```text
POST /api/auth/guest
  → insert a users row with every identity column NULL, and stamp last_login_at
  → sign an HS256 JWT: { user_id: <id>, phone: null, exp: now + JWT_EXPIRES_IN, iss: "kada" }
  → 200 { token, user: { id, phone: null } }

GET /api/me                       Authorization: Bearer <token>
  → 200 { user: { id, phone: null } }
```

Three properties of that are deliberate:

1. **The row is written before the visitor has done anything.** Every page load that finds no token costs
   one insert, which is why the route sits behind the strict limiter and, inside it, the tighter guest one
   - two ceilings on one endpoint, the second bounding exactly what the first is too loose for.
2. **The token is the account.** There is no password to reset and no second factor to lose, so this
   bearer token is the only credential the system has: HS256 only, expiry required (see
   [8.4](#84-security-posture)). Using a stolen one is indistinguishable from using a real one, which is
   the cost of not asking anybody who they are.
3. **`phone` is always null.** It travels because the column does, and because an older build could have
   written one; the API answers it rather than pretending the field does not exist.

A browser that cannot reach `POST /api/auth/guest` gets no session at all, and the shell says so instead of
rendering an empty page - see [6.4](#64-there-is-no-sign-in-page).

### 7.3 Recording a click

```text
GET /r/:code
  → resolve the link (cache, then database)
  → answer: 302 to the destination, or the guide page (WeChat, QQ, Xiaohongshu)
  → a goroutine of its own records the click

WriteClick, in one transaction:
  INSERT click_logs ... ON CONFLICT (event_id) DO NOTHING
      inserted, kind = visit → UPDATE links SET click_count = click_count + 1
      inserted, any other kind → commit only (recorded, not counted)
      conflict → commit only (the click was already counted)
```

Recording happens on a goroutine of its own: the redirect handler must not wait for a database round trip
before answering, so it spawns the work and returns. That has a consequence the code has to be written
around - Gin's recovery covers only the goroutine that serves the request, so **anything that panics here
ends the process**. `LogClick` recovers and counts the panic; the click is lost, the API is not.

`event_id` is a unique key and the insert is `ON CONFLICT DO NOTHING`, so a click that arrives twice - a
retried write, a request that timed out and was answered - is counted once. It survives from the phase when
the row was written by a queue consumer rather than here, and it costs one column to keep.

**Why there is no queue any more.** The pipeline used to publish to Kafka and have `cmd/worker` write the
rows, on the argument that the redirect is hot and the click is cold. That argument was never tested by
reality: the deploy never set `KAFKA_BROKERS` and no worker unit existed, so production ran the direct write
the whole time, and the broker was a second write path, a second binary and five metrics that only the
compose stack could exercise. It was removed (see [7.5](#75-what-the-numbers-turned-out-to-be) for what the
numbers actually showed). The comment that used to explain how a nil publisher took the API down is gone with
the interface it was about.

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

Nothing on the guide page navigates by itself. It exists because the in-app browser cannot follow the link,
so an automatic attempt can only land on that browser's blocked-link screen - and it did, two seconds in,
taking the instructions with it. The page now stays until the visitor taps something: the deeplink button, the
copy button, or the QR code. The same rule removed the 800ms fallback inside the deeplink attempt, which
navigated before the failure was even visible.

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
HTTP; a 302 runs two statements in one transaction per click. Raising the working set from 200 to 2,000
codes moved throughput to 1,776, which rules the cache and the size of the working set out as the
constraint and leaves the write volume as the ceiling - about 1,600-1,800 transactions a second on the
machine this was measured on.

That number is also the argument against the queue that used to sit here. A broker moves the write off the
request path, and 1,700 clicks a second is not a volume this product has ever seen; what it cost was a
second write path, a second binary and a broker in the compose stack. It was removed in favour of the
direct write, which is what the measurement above was actually made against.

### 7.6 A blocked target is refused, and the refusal is not cached

A short link is a promise that the URL behind it is safe to open, and that URL is chosen by whoever created
the link. The categories that break the promise - 色情, 赌博, 毒品, 诈骗, 钓鱼 - are not something a code
change can recognise, so they live in `blocked_hosts`: a table an operator edits, with the category that
made the row. Matching is a suffix match on the host, so one row for `example.com` covers every subdomain a
campaign mints against it.

The list is read into memory (an `atomic.Pointer` swapped whole, so a request sees one list or the next and
never a half-built map) and refreshed every 30 seconds. That matters because the redirect path is the hot
path and cannot afford a query per request - but it also decides *where* the question is asked. It is asked
on every redirect, not folded into the cached link: a link cached while its host was allowed must stop being
served the moment the host is blocked, and a decision cached with the link would keep serving it. The same
question is asked on `Create` and `Update`, before a short code is allocated, and in `VerifyPassword`, because
a correct password is not permission to reach a blocked target.

The answer is a `403` with the category named, not a `404`. The link exists and the visitor did reach it, and
"not found" would leave whoever reported it unable to tell a takedown from a mistyped code. A false positive
is a complaint that has to be argued with, and "blocked" alone gives nobody anything to argue about.

Private and reserved addresses are a different rule in the same place: `urlcheck.IsSafeTarget` refuses a
target that is loopback, RFC 1918, link-local or one of the other reserved ranges, because a short link
aimed at `192.168.1.1` asks the *visitor's* browser - not ours - to open their router or an internal panel.
The ranges are shared with the preview fetcher, which also resolves the host before dialling; the create
path does not resolve, because a DNS lookup that can fail, be slow or answer differently later has no place
in the path that allocates a short code.

Adding a row is an insert, and needs no deploy:

```sql
INSERT INTO blocked_hosts (host, category, note, created_at)
VALUES ('casino.example', 'gambling', 'reported 2026-10-22', now());
```

### 7.7 A takedown is a column and a history row

The host list stops whole domains. It cannot stop *one* link on a legitimate host, which is what most
reports are about - a shared blog, a file host, a page that turned. That decision belongs to a person, so it
is stored in two places with two different jobs:

- `links.blocked_at` is the state. The redirect already reads that row (or its cached copy) to answer "may
  this be served", so the check costs nothing extra, and `Block` drops the cached copy after it commits -
  which is what makes a takedown take effect on the next request instead of at the end of the TTL.
- `link_blocks` is the history: who stopped it, when, why, on what evidence, and when and by whom it was put
  back. A column that is nulled on unblock answers none of that, and the owner whose link went down asks
  first. The two are written in one transaction, so they cannot disagree about the present.

`abuse_reports` is where a takedown usually starts. `POST /api/reports` is public - the person who needs it
was sent somewhere illegal by a short link and has no account here, and asking them to sign in first is
asking them to do our paperwork - so it is rate limited on the strict bucket and bounded in what it accepts.
The report outlives the link: `LinkID` is nullable and the short code is copied onto the row, because
evidence that disappears when the reported thing is deleted is not evidence. `GET /api/admin/reports`
returns each report with whether its link is stopped now, which answers "has this been dealt with" without a
second status column that could disagree with it.

The operator routes are `POST /api/admin/links/:code/block`, `.../unblock`, `GET .../blocks` and
`GET /api/admin/reports`, mounted behind `ADMIN_PHONES` (comma separated). The list holds phone numbers
rather than user ids because that is what the operator signs in with, and it is checked against the phone
claim rather than by looking the account up - so a `kada_` API token cannot moderate anything, because it
carries no number. An empty list refuses everybody: a missing environment variable must not be the thing
that hands out the takedown routes.

What this deliberately does not do: there is no admin page (four curl commands, and a console is the sort of
thing that grows), no automatic category detection (that is a third-party URL-safety API, and a wrong
verdict from one is a link pulled for no reason), and the 24-hour response time the report page states is a
promise made by whoever runs the deployment, not a job that runs by itself.

### 7.8 What one account may create

`auth/guest` writes a real account row on every call, so minting an identity costs one request and minting
links after that costs nothing - which used to mean one address could turn itself into 120 accounts a minute
and each account into any number of links, none of them attached to anybody. Two ceilings now apply, and both
are checked in `Create` before a folder is looked up or a short code allocated:

- **A daily allowance** of 20 links per rolling 24 hours, for every account. There is one allowance
  because there is one kind of account: a claimed phone number used to buy a larger one, and the site has
  no phone numbers any more.
- **A burst ceiling** of 5 links per rolling 10 minutes, for everybody. The daily quota bounds a day and this
  bounds a moment; a script needs the moment, and it is what stops a new account from spending its whole
  allowance in the minute it was created.

A refusal is a 429 carrying the message and a `Retry-After` computed from the oldest link still inside the
window, so it says when there is room again instead of "later". A failure to read the counts allows the
create: the ceiling exists to stop a script, and a database that cannot answer a count would refuse the
insert a moment later anyway - turning one bad query into "nobody can make a link" is the worse failure.

`auth/guest` also has a per-address ceiling of its own, ten a minute.

Every limiter counts in a key of its own, `ratelimit:<name>:<ip>`. That is not cosmetic: they used to share
`ratelimit:<ip>`, so the strictest limit - twenty a minute on the auth routes - was really a limit on every
request the address made, and a visitor who read the dashboard for a minute could not then sign in. The name
in the key is the whole fix.

### 7.9 A link can have a pool of destinations

`links.original_url` answers "where does this go" for every link that has one place to go, which is most of
them. A link may instead carry a pool in `link_targets`, and a visitor is sent to one of them:

- **weight** decides the share, and a weight of zero means "in the pool but never chosen" - which is how a
  target is paused without deleting it and losing its history.
- **daily_quota** caps how many visitors one target takes in a day, after which the pool skips it. That is
  what a 群活码 needs: a group fills up, the next one takes over, and the QR code already printed never
  changes.
- **enabled** takes a target out of the draw.

The pool is replaced whole (`PUT /api/links/:id/targets`), and each replacement writes one row into
`link_target_audits` with the before and the after - who changed the split, when, and from what to what. A
row-by-row edit has no moment at which a decision could be recorded, which is why there is only one write
path.

The pool travels with the link through the cache, because the redirect draws from it on every request. That
makes two things necessary: a replacement drops the cached link, and a pool URL is validated again when it is
served. The checks are cheap, and cached data is what they exist to distrust.

A link whose pool cannot take this visitor - every target disabled, full, or weighted zero - answers 404
rather than falling back to `original_url`. The link has said where its traffic goes; `original_url` is where
it pointed before it had a pool, and sending a visitor there is the stale-URL failure the split exists to
avoid.

The counter behind `daily_quota` is in memory. It is a routing decision rather than a bill, one API instance
makes it exact, and several would make it approximate - bounded by the day's quota per instance.

Which target took a click is on the click row (`click_logs.target_id`), and the owner's target listing is a
group-by over it, which is why the counts are a request of their own rather than part of the link.

### 7.10 A short link can be a file

A file link is an ordinary link whose target happens to be a file this service stores. `POST /api/uploads`
takes the file and answers a URL on this domain (`/f/<id>`), and creating a link with that upload's id makes
the service derive the target from it - the caller does not get to type one. The point is the domain: a
"short link" pointing at somebody else's image host is the thing this replaces.

The file is served with a year of `immutable` caching, because the id is unguessable and the contents never
change, and with `Content-Disposition: inline` for images and PDFs only: an image has to be visible to be
saved by hand, a PDF is what the browser's viewer is for, and an Office file cannot be rendered at all - so
offering it inline would be a blank page. `?download=1` forces a download.

Two things make this safe to expose. The type is decided by the extension **and then checked against the
first bytes**, so a `.png` that starts with a ZIP header is refused; and the only types accepted are png,
jpg, webp, gif, pdf, docx, xlsx and pptx - video and audio are out of scope, and refused by type and by the
20 MB per-file ceiling.

Storage is the local disk (`UPLOAD_DIR`), not a bucket: one machine, files written once, a directory that
survives a deploy. The per-account ceiling is 50 MB, one allowance for the same reason the link quota is
one (see [7.8](#78-what-one-account-may-create)).

Deleting a link does not delete its file. It orphans it, and an hourly sweep removes uploads that no link
refers to once they are a day old. Sweeping rather than deleting at the point of deletion keeps the link
code out of the file business, and the grace period is what stops a link being created right now from losing
its file to a sweep that ran in between.

### 7.11 A group code is a target that expires

A WeChat group's QR code dies seven days after it is generated and stops admitting people once the group is
full. That is the product behind `kind` and `expires_at` on a target: the short link and the printed poster
never change, and what changes is which group the pool is pointing at.

A target whose kind is `wechat_group` is **shown, not redirected to**. The page puts the code on screen with
the instruction that makes it work (long-press to recognize, or screenshot and scan from the album) - a 302
would hand the visitor an image file with nothing telling them what to do with it. The page confirms the
visit with the same handshake the guide page uses, because WeChat fetches a link to build a preview card
without showing it to anybody, and running the script is the only evidence a person opened it.

An expired target is skipped by the draw rather than deleted: the row is still the record of what that code
was, and replacing it is editing the row. The owner sees `有效期至 <date>` on the target, in amber inside the
last three days - which is the warning, on the row where the replacement happens.

When a group pool has nothing left to serve, the answer is a 200 page saying the group is full or the code
has expired. A 404 was right for a plain pool (#42): the link said where its traffic goes and nothing there
was usable, so it is broken. Here "every group filled up" is a state the owner asked for, and telling the
visitor their link is broken would be a lie they cannot act on - so the metric counts `group_empty` apart
from `not_found`.

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

Sliding-window counters in Redis, in five tiers: a global limit for normal API traffic, a strict limit for
auth endpoints, a higher-throughput limit for redirection, a tighter one for the guide page's action reports,
and a tighter one still for creating an account. Each tier has a key of its own (`ratelimit:<name>:<ip>`);
they used to share one, which made the strictest tier a limit on all traffic from that address. The client IP
comes from `X-Real-IP`, which nginx rewrites. `X-Forwarded-For` is not trusted, because a client can forge it.

The action tier is the only one mounted on a single route rather than on a group, and its ceiling is the
lower one because nothing a visitor sees depends on those reports: they are recorded, never counted, so a
limit that drops one costs a line of engagement detail, while what it bounds is a script manufacturing
activity on somebody else's link. The visit confirmation stays under the redirect limit despite being a
POST - it is what makes a click count, so dropping one loses a real visit - and its volume can only ever be
a fraction of the redirects that served the page it reports from.

### 8.4 Security posture

- JWT: HS256 only, expiry required, secret strength enforced in release mode.
- Link targets: only `http` and `https` are accepted, which also blocks `javascript:` and `data:`
  payloads from the guide page.
- Link preview: the fetcher refuses internal networks and cloud metadata addresses (SSRF).
- CSV export: fields are escaped and formula-injection prefixes are neutralised.
- Error responses never echo internal errors.
- Containers run as a non-root user; nginx sets the usual response security headers.

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
| `UPLOAD_DIR` | `./data/uploads` | Where file links are stored; must be writable and outlive a deploy |
| `ADMIN_SECRET` | empty | The operator password; empty refuses everybody |
| `VITE_API_URL` | `""` (same origin) | API base for the browser, fixed at build time |

`config.Config` is that list and nothing else: one struct, parsed once, and no other file reads the
environment. Two of them carry a cost for being wrong, in opposite directions. An empty `ADMIN_SECRET`
refuses every operator route - the safe direction for a missing value - while a wrong one is
indistinguishable from a right one until a takedown is attempted. An unwritable `UPLOAD_DIR` stops the
process at startup rather than failing on somebody's first upload.

None of these are typed into the server by hand. The deploy job writes `ADMIN_SECRET` into
`/opt/kada/backend/.env` with `deploy/upsert-env.sh`, as its **first** step, before a single service is
replaced; everything else in that file is left as the host has it. `--set` with an empty value is a no-op,
so a repository secret that is not set cannot blank a value the host already holds, and `--require` is
what turns "nothing there at all" into a failed deployment. The check reads **the file**, not the
repository secrets, which is what keeps a hand-configured host deployable: the value only has to reach the
file by some route.

Only `ADMIN_SECRET` is written that way today. It is not `--require`d, on the argument that if the
operator secret is missing the console can simply refuse to open - refusing the whole deploy would hold
every unrelated fix hostage to an operator feature.

`SITE_ORIGIN` (`vars.SITE_URL`, default `https://kada.click`) is not a service setting: the deploy uses it
to ask this host's nginx for the site without depending on DNS.

## 10. Deployment

Three supported shapes:

1. **Docker Compose** (`docker compose up -d`): nginx, API, web, PostgreSQL and Redis on one host.
   Suitable for a single server or local development.
2. **systemd + released binaries** (`deploy/`): the API runs as a native process, with PostgreSQL and
   Redis provided separately. This is what the GitHub Actions deploy job uses: it builds the binary,
   applies the schema with `bin/migrate`, restarts `kada-api`, and verifies the health endpoint, rolling
   back to the previous binary if the service does not come up.

The secrets reach the host from **GitHub repository secrets**, written into `/opt/kada/backend/.env` by
`deploy/upsert-env.sh` as the first step of the deploy, before any service is replaced. That file is the
fix for a whole class of outage: a credential that never worked turned out to be an empty line in a file
on the server that nothing had ever checked. An empty `--set` leaves an existing hand-configured value
alone, and a `--require`d key that is missing fails the deployment while the previous build keeps serving.

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

Renaming the shell into place is only half of it: the browser has to ask for it again. `/assets/` is served
with `expires 365d` and `immutable`, which the content hashes make safe, and `index.html` is served with
`expires -1` - `Cache-Control: no-cache`, so every visit revalidates it and an unchanged file still answers
304. Without that second half nginx sends only `Last-Modified` and `ETag`, and a browser is free to apply
heuristic freshness of roughly ten percent of the time since the file changed: a deploy inside that window
is invisible, because the cached shell points at a bundle that is still on disk and still works. That is
not hypothetical - on 2026-10-21 the host had the new build and the site still looked a version behind.
`expires` is used rather than `add_header Cache-Control` because a location that sets `add_header` stops
inheriting the server block's security headers, and `location /` serves the one response they matter most
for.

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
| Frontend | Vitest for pure helpers and the UI primitives, rendered to static markup - no DOM, no jsdom |

Run everything with `go test ./... -count=1 -race` and `cd web && npm test`; CI
additionally runs `go vet`, `golangci-lint`,
`tsc --noEmit` and the production build on every pull request.

## 12. Known limitations

These are deliberate trade-offs, not oversights:

- **Analytics are computed on demand.** `click_logs` is queried directly; there is no rollup table.
  Acceptable at the current volume, and the obvious first thing to change if the tables grow.
- **The click write is synchronous inside its goroutine.** Each click is two statements in one
  transaction on a goroutine of its own, which is what caps the redirect path at about 1,600-1,800 a
  second on the machine the baseline was taken on. A queue was built for that ceiling and removed when it
  turned out to be deployed nowhere (see [7.3](#73-recording-a-click)); the next version of this is
  batching the writes, not a broker.
- **AutoMigrate cannot express every future change.** Renames, type narrowing and data backfills need
  a hand-written statement; there is no down-migration path.
- **No background job scheduler.** Expiry is evaluated at read time rather than by a sweeper.
- **Single-tenant deployment.** There is no organisation or role model: an account owns its folders, tags
  and links and shares them with nobody. The `workspaces` table was the place a team model would have gone,
  and it was removed rather than kept warm - it grouped links without granting access, which is a filter
  folders already provided.
- **Rate-limit state is per-Redis-instance**, so a multi-instance deployment needs a shared Redis
  (which is the intended topology).
- **An account is a browser, and stays one.** Its token lives in one browser's storage, so a cleared
  browser or an expired token means a new account and the old one's links are unreachable: there is no
  identifier to look them up by, and nothing to claim them with. Anonymous creation is also an open write
  path - `POST /api/auth/guest` sits behind the strict limiter and the guest tier, and the quota in
  [7.8](#78-what-one-account-may-create) bounds what one of those accounts can then create.
