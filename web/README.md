# Web

A Vite + React single-page app. It talks to the Go API over HTTP and nothing else: `src/lib/api.ts` is
the one place that knows the endpoints and attaches the bearer token.

```bash
npm ci
npm run dev      # http://localhost:3000, with /api/* proxied to the Go API on :8080
npm run build    # dist/, plain static files
npm test         # vitest, for the pure helpers
npm run lint     # eslint
```

| Path | What lives there |
|---|---|
| `src/router.tsx` | The route table - the only place that says which URLs exist |
| `src/pages/` | One file per route |
| `src/components/` | The shell (sidebar, top bar, language capsule, theme toggle) and shared pieces |
| `src/lib/` | API client, auth, i18n dictionary, theme, and the pure helpers the tests cover |
| `src/globals.css` | Tailwind entry point and the theme tokens |
| `index.html` | The document shell |

The theme script is injected into `index.html` at build time by `vite.config.ts`, from the one
implementation in `src/lib/theme/bootstrap.ts`: it has to run before the first frame, which a component
rendered by React cannot do.

The build is static files, so production has no Node process: nginx serves `dist/` directly. See
`deploy/nginx/nginx-prod.conf` and the deployment section of `docs/design.md`.