# Code Style

Conventions used in this repository. They follow what the code already does, so the goal is
consistency rather than reform. When something here contradicts the code, the code wins - and the
document should be fixed in the same pull request.

Tooling already enforces most of this: `gofmt` and `goimports` through `golangci-lint`, ESLint and
Prettier-style formatting on the frontend. What tooling cannot check is naming, structure and the
quality of a comment, which is what the rest of this document is about.

## Contents

- [1. General principles](#1-general-principles)
- [2. Comments](#2-comments)
- [3. Go](#3-go)
- [4. Go: errors](#4-go-errors)
- [5. Go: layering](#5-go-layering)
- [6. Database (GORM)](#6-database-gorm)
- [7. TypeScript / React](#7-typescript--react)
- [8. Frontend copy and i18n](#8-frontend-copy-and-i18n)
- [9. Tests](#9-tests)
- [10. Configuration and secrets](#10-configuration-and-secrets)
- [11. Formatting quick reference](#11-formatting-quick-reference)

## 1. General principles

- **Optimise for the next reader.** Code is read far more often than it is written. A slightly longer
  name or an extra comment costs nothing; an unexplained decision costs hours.
- **Match the neighbours.** A new file should be indistinguishable in style from the file next to it.
- **Prefer boring, explicit code.** No clever indirection, no reflection, no generics where a loop
  would read better. This codebase is a straightforward layered application and should stay that way.
- **One reason to change per file.** A service handles one resource; a handler exposes one resource.
- **Delete rather than comment out.** Git remembers.
- **No dead code, no unused exports, no "just in case" parameters.**

## 2. Comments

A comment explains **why**, never **what**. The code already says what it does.

```go
// Good: the reader cannot guess this rule from the statement.
// A custom short code gets a friendly error, but a random one is regenerated and retried,
// so a check-then-insert race resolves itself instead of failing the request.
if isDuplicateKey(err) { ... }
```

```go
// Bad: restates the code, and the next edit makes it a lie.
// Check if duplicate key error.
if isDuplicateKey(err) { ... }
```

Write a comment when:

- The reason for a choice is not obvious (a workaround for a third-party bug, a deliberate
  security trade-off, an ordering constraint).
- A rule was tightened because of a specific past bug. Say what the old behaviour was, briefly.
- A number is a decision: `// bcrypt caps at 72 bytes` beats a mystery `72`.

Do not write a comment when:

- It restates a signature or a well-named function.
- It is a changelog ("added X", "removed Y"), or a reference to a ticket number that means nothing
  outside that ticket.
- It documents behaviour the function does not have. Fix the code or the comment, never leave the
  contradiction.

Exported identifiers in Go carry a real doc comment (a full sentence starting with the name) because
linters require it and because `go doc` shows it.

Section banners are used sparingly in longer files:

```go
// ==================== Auth ====================
```

## 3. Go

### Naming

- `camelCase` locals, `PascalCase` exported, `snake_case` only in JSON and database tags.
- Initialisms keep their case: `userID`, `shortURL`, `apiToken`, but never `Url`/`Id`.
- Interfaces that a consumer declares are named for the behaviour, not the implementation:
  `AuthService`, `ClickWriter`, `ClickPublisher`.
- Booleans read as a statement: `isActive`, `hasPassword`, `slugTaken`.
- Package names are short, lower case, no underscores: `auth`, `link`, `folder`, `utm`.

### Declarations

- Group imports into two blocks separated by a blank line: standard library and third party first,
  then this module's packages (`github.com/booondenz/kada/...`). `goimports` with
  `local-prefixes` enforces the split.
- Declare the zero value as the useful default when you can: `var total int64` over an arbitrary
  initialiser.
- Prefer a table (a slice of structs) over a chain of `if` when validating a set of cases.
- Keep functions short enough to hold in your head. If a function needs section comments, it is
  usually two functions.
- Return early instead of nesting: guard clauses first, happy path last.

### Types and pointers

- Use a pointer field when "absent" is meaningfully different from the zero value, and say so in the
  struct tag (`*string` for a nullable column, `*bool` for a tri-state update).
- Never take an address of a loop variable, and never return a pointer to a local that a caller might
  mutate by accident.
- Convert between layers with an explicit mapper (`toUserInfo`, `linkInfoFromEntity`) rather than
  sharing a struct across layers with tags for both.

### Concurrency and context

- Every function that talks to the network or the database takes `ctx context.Context` as its first
  parameter and uses `db.WithContext(ctx)`.
- Do not start a goroutine without a way for it to stop.
- Sharing a value across goroutines means it is immutable or guarded; there is no third option.

### Security-shaped style

- Never build SQL by string concatenation of user input; pass values as parameters.
- Never trust a header or an ID from the client for an ownership decision - check `user_id` in the
  query.
- When an error leaves the system, log the cause and return a generic message.

## 4. Go: errors

- Wrap with context and `%w` so the chain survives:
  `fmt.Errorf("failed to send SMS: %w", err)`.
- Return a sentinel only when a caller must branch on it, and put it where both layers can import it
  without a cycle (`domain.ErrEmailTaken`).
- Messages are lower case, no trailing punctuation, no "oops", and safe to show a user.
- Handle every error, or discard it explicitly with `_ =` and a comment saying why it is safe. Silent
  discards are the one thing reviewers consistently reject.
- Do not log and return the same error at every level; log once, where the context is richest
  (usually the service), and add context on the way up.

```go
// Good
if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
    log.Printf("create workspace %q for user %d failed: %v", row.Slug, row.UserID, err)
    if isDuplicateKey(err) {
        return nil, ErrSlugTaken
    }
    return nil, fmt.Errorf("failed to create workspace: %w", err)
}
```

## 5. Go: layering

The rule that keeps the codebase testable:

| Layer | May | Must not |
|---|---|---|
| `handler` | Bind and validate JSON, map errors to status codes, call one service | Run SQL, contain business rules, import another handler |
| `service` | Business rules, ownership checks, transactions, cache invalidation | Touch `*gin.Context`, format HTTP responses |
| `infra` | Talk to an external system | Know about a specific handler or route |
| `domain` | Structs, sentinels, pure helpers | Import a service, a handler or `infra` |

A handler declares the slice of the service it needs as its own interface. That is what allows
handler tests to run without a database:

```go
type AuthService interface {
    SendSMSCode(ctx context.Context, phone string) error
    // ...
}
```

Ownership is enforced in the service, in the query:

```go
// Good: a row belonging to somebody else simply does not match.
tx.Where("id = ? AND user_id = ?", folderID, userID).First(&row)
```

```go
// Bad: fetch first, compare later - and one forgotten comparison is an authorization bug.
tx.Where("id = ?", folderID).First(&row)
if row.UserID != userID { ... }
```

## 6. Database (GORM)

- Models live in `internal/domain/entity` and are the schema. Transport structs live in
  `internal/domain/models.go` and are not the schema. Keep the two apart.
- Declare `type:` explicitly when the Go type would pick the wrong column type, and name every index,
  unique constraint and `ON DELETE` rule you rely on.
- Put `constraint:` on the **association** field, not on the foreign-key field.
- A pointer column means nullable; a non-pointer column means `NOT NULL`.
- Prefer the query builder for anything a reader can follow:

```go
var rows []entity.Link
err := s.db.WithContext(ctx).
    Where("user_id = ?", userID).
    Order("created_at DESC").
    Limit(pageSize).Offset((page - 1) * pageSize).
    Find(&rows).Error
```

- Reach for raw SQL only when a single statement is genuinely the point, and say so in a comment:
  atomic consume-and-return, `ON CONFLICT DO NOTHING` idempotency, bulk `INSERT ... SELECT`. The
  existing examples are in `auth_service.go`, `click_store.go` and `link_service.go`.
- Bound parameters only. Never concatenate a value into SQL, including an `ORDER BY` - map a sort key
  to a fixed string instead:

```go
orderBy := "l.created_at DESC"
switch sort {
case "clicks_desc":
    orderBy = "l.click_count DESC"
}
```

- GORM drops a field from an `UPDATE` when it is nil, and also when it points at a zero value. When
  the difference matters (`is_active = false`, `folder_id = NULL`), build an explicit `map[string]any`
  and pass real values, using `gorm.Expr("NULL")` to clear a column.
- An update that must report "no such row" needs its own check, because `Updates` does not error on
  zero rows:

```go
res := tx.Model(&entity.Folder{}).Where("id = ? AND user_id = ?", id, userID).Update("name", name)
if res.RowsAffected == 0 {
    return gorm.ErrRecordNotFound
}
```

- Read back a row after updating it inside the same transaction, so the caller never receives a
  half-updated value.

## 7. TypeScript / React

- TypeScript strict everywhere; no `any`. Use `unknown` and narrow it, as the existing `catch`
  blocks do.
- Components are function components. `"use client"` only where interactivity or browser APIs
  require it.
- Server state comes from SWR hooks; all HTTP goes through `src/lib/api.ts`, which is the only place
  that knows the endpoint shapes and attaches the bearer token. Do not call `fetch` in a component.
- Local UI state stays in the component; anything shared goes in a context provider
  (`I18nProvider`).
- Keep components small and named exports for helpers. Presentational pieces live in
  `src/components`, route entries in `src/app`.
- Tailwind utility classes inline, with `className` composition kept readable; prefer the existing
  helpers (`inputBase`, `fieldState`) over repeating long class strings.
- Effects need a cleanup path when they hold a resource - dispose WebGL textures, clear intervals and
  cancel listeners.
- Anything read from `localStorage` during render must be behind `useHydrated()`. A Client Component is
  still prerendered on the server, where storage does not exist, so rendering it directly makes the two
  renders disagree and React discards the tree.
- Frontend tests use Vitest and cover pure logic (geometry, parsers, utilities), not rendered layout.

### Colours

Write the role, not the shade:

| Use | Not |
|---|---|
| `bg-canvas`, `bg-subtle`, `bg-muted-surface` | `bg-white`, `bg-gray-50`, `bg-gray-100` |
| `text-strong`, `text-body`, `text-muted`, `text-faint` | `text-gray-900`, `text-gray-700`, `text-gray-500`, `text-gray-400` |
| `border-line`, `border-line-strong` | `border-gray-200`, `border-gray-300` |

Both resolve to the same colour in the light theme, so the change is invisible - and in the dark theme
the role is what makes it correct. Brand and status colours (`bg-indigo-600`, `text-red-600`) are left
alone on purpose: they are fixed in both themes, so a pair like `bg-indigo-600 text-white` cannot break.

Two traps this convention exists to avoid, both of which shipped once and were caught in review:

- **A themed surface on a permanently dark screen.** The sign-in screens are dark in both themes, so the
  selected segment must be literal `bg-white` with dark type. Given `bg-canvas` it turned black in dark
  mode and the label - whose colour is fixed - went with it.
- **A colour whose partner did not move.** Redefining a whole palette for dark mode breaks every
  `bg-X text-white` pair whose `X` got brighter. Redefine roles, not ramps.

## 8. Frontend copy and i18n

The UI is bilingual and the dictionary is keyed by the Chinese source string:

```tsx
const t = useT();
<label>{t("手机号")}</label>
```

- Every user-facing string goes through `t()`.
- Add the English entry to `web/src/lib/i18n/dictionary.ts` in the same change.
- Never build a sentence by concatenating translated fragments; translate the whole sentence.
- `zh` is the identity map, so a missing key degrades to Chinese instead of breaking the page. That
  is a safety net, not a licence to skip a translation.
- Dates and numbers are formatted for the active locale, not with a hardcoded locale.

## 9. Tests

- Table-driven tests with a `reason` field, so a failure names the case:

```go
tests := []struct {
    code    string
    isValid bool
    reason  string
}{
    {"abc123", true, "alphanumeric"},
    {"", false, "empty string"},
}
```

- Test the behaviour, not the implementation: assert on a returned value or a generated SQL string,
  not on private state.
- A test name says what is guaranteed: `TestUserHidesPasswordHash`, not `TestUserStruct`.
- No database in unit tests. Use a `DryRun` GORM session to assert generated SQL, and fakes for
  service interfaces.
- A bug fix should come with the test that fails without it.
- Do not skip or comment out a failing test; fix it or delete it.

## 10. Configuration and secrets

- `config.Load()` in `config` is the only place that reads the environment. Adding a setting
  means: a field on `Config`, a default (or a comment saying why there is none), a row in
  `.env.example`, and a row in the README table.
- Never commit a real secret, and never log one. Log a masked value when you must identify it
  (`maskPhone`).
- A placeholder value in an example file is a trap: it looks configured and fails at runtime. Leave
  optional credentials empty, and detect the placeholder case explicitly where it is cheap.
- Weak defaults are refused in release mode rather than trusted.

### Empty means something different from unset

`??` accepts an empty string, `||` does not. Pick deliberately and write down which one you mean,
because the difference is invisible until it breaks:

```ts
// unset -> development default, "" -> the empty string, a value -> that value
const API_URL = import.meta.env.VITE_API_URL ?? "http://localhost:8080";
```

A hardcoded default in the framework config used to supply that empty string, which applied to local
development too, where it silently beat the fallback and sent every request to the dev server - an HTML
404 instead of JSON.

The follow-up is worse than the original bug: **the bundler replaces a build-time variable, and "unset"
and "" are not the same thing.** A source that reads an empty string as "use relative requests" and an
undefined value as "use the development default" produces two different behaviours from one build. An
empty value therefore cannot carry meaning across a build.

The rule in this repository: **use an explicit sentinel, not an empty string**, when a build-time
setting needs to mean something:

```ts
const SAME_ORIGIN = "same-origin";   // inlined reliably, unlike ""
```

And remember that `VITE_API_URL` is fixed at build time: set it as a build argument
(`web/Dockerfile`, `docker-compose.yml`) or in the build step (`ci.yml`), never as a container
runtime environment variable, where it does nothing at all.

## 11. Formatting quick reference

| Concern | Rule |
|---|---|
| Indentation | Tabs in Go (`gofmt`), 2 spaces in TS/CSS/JSON/Markdown |
| Line length | ~100 columns in Go, ~100 in TS; comments wrapped at ~80 |
| Trailing commas | Required in multiline TS literals (Prettier) |
| Blank line | Between logical blocks, not after `{` or before `}` |
| Import order | stdlib, third-party, then `github.com/booondenz/kada/...` |
| File endings | LF, one trailing newline (enforced by `.gitattributes`) |
| Commit messages | English, one line, `type: imperative summary` - see [contributing.md §6](contributing.md#6-commit-messages) |

Run before pushing:

```bash
golangci-lint run --timeout=5m   # gofmt, goimports, govet, staticcheck, gosec, misspell
cd web && npx tsc --noEmit && npm run lint
```
