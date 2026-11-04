# web
Purpose: React SPA for Ervisio Logs (sign-in, workspaces/projects, log explorer + live tail, alerts, channels, storage admin, tokens, members, bundled docs), served by the Go backend from embedded `web/dist`.

Stack: Vite 8, React 19, react-router-dom 7 (BrowserRouter), TypeScript 6, Tailwind 4 (`@tailwindcss/vite`, tokens in `src/index.css`), i18next (en/it), lucide-react, Vitest+jsdom+RTL, Playwright. No data/query lib: plain `useEffect` + `AbortController`.

Commands (cd web): `npm run dev` (proxies /api,/healthz,/readyz to `VITE_BACKEND` or 127.0.0.1:8080) | `build` (tsc -b && vite build) | `typecheck` | `lint` | `test` (vitest run) | `e2e` (needs E2E_BASE_URL + E2E_SETUP_CODE, run via `scripts/e2e.sh`).

Entry points: `src/main.tsx` (initTheme, i18n, fonts) → `src/App.tsx:App` (routes) | `src/lib/api.ts:api,request,ApiError,setCsrfToken,tailUrl` | `lib/auth.tsx:AuthProvider,RequireAuth,safeNext` | `lib/tenancy.tsx:TenancyProvider,useTenancy` | `lib/sections.ts:SECTIONS` | `components/Shell.tsx` (rail/nav/topbar via outlet ctx `ShellContext.topbar`) | `lib/query.ts` (search-box parser) | `lib/useReadiness.ts`.

Flow:
- `/login` → pages/Login (local + OAuth providers, first-admin setup w/ setup_code) → api.providers/login/register → /api/v1/auth/*
- RequireAuth (api.me → /auth/me) → TenancyProvider (api.projects?workspace_id=) → Shell
- `/` Home opens the last project; canonical `/w/:workspaceId/p/:projectId[/streams/:streamId]` → Explorer/Logs → POST /logs/search. `/w/:workspaceId/projects` lists/creates; unknown scoped workspace returns 404.
- Canonical project sections: alerts, tokens, legacy-compatible live/logs/storage. Workspace `/w/:workspaceId/members|channels`; instance `/admin/storage` needs no project. `/projects/:projectId/*` LegacyProject resolves membership then redirects, preserving query/hash. Unavailable sections return 404 and stay absent from navigation.
- `/account` provides profile, password change and session revocation; `/forgot-password`, `/reset-password`, `/verify-email`, `/invite` and `/accept-invitation` are public email flows.
- `/admin` is instance health; `/admin/users` manages users; `/admin/tokens` manages scoped instance admin tokens; `/admin/settings`, `/w/:workspaceId/settings`, `/w/:workspaceId/p/:projectId/settings` expose settings.
- `/status` Overview (useReadiness polls /readyz every 10s) | `/docs[/:topic]` Documentation | `*` NotFound
- Export: api.exportLogs → POST /logs/export (blob; ndjson|csv)

Invariants:
- Backend contract: `/api/v1`, cookie session (`credentials: same-origin`), `X-CSRF-Token` on every non-GET (token from Session.csrf_token, held in module var, cleared on logout/401), errors are RFC 7807 (`detail`) → ApiError(status, retryAfter).
- Auth bootstrap is aborted when establish/logout/retry supersedes it, preventing stale session or CSRF state from overwriting a new login. Invitation preview waits for authenticated/anonymous session readiness.
- Email tokens arrive in URL fragments, are removed from the address and sent only in POST bodies; invitation continuity through login uses SPA module memory, so refresh requires the original link.
- Settings use server key/type/value/source/inherited/locked metadata, respect env locks and workspace owner write access, and save per key; partial failures retain unsaved drafts. Project settings also edit stream/environment metadata.
- Admin user self-mutations are disabled; server conflicts remain visible. Admin tokens require allowed scopes and future expiry, and the one-time secret is cleared when its dialog closes.
- Shared `packages/admin-client` is transport-neutral: web supplies cookie/CSRF request, plugin supplies socket or HTTPS transport.
- Auth: only 401 from /auth/me means anonymous; 5xx/network → "unreachable" screen with retry (never bounce to login). Any 401 in tenancy calls auth.retry(). `?next=` goes through `safeNext` (same-app paths only; rejects `//`, `/\`).
- Workspace scoping: URL workspace wins; unknown URL workspace never falls back. Unscoped routes use localStorage workspace then first membership. Project lists keyed workspace+attempt reject stale/foreign responses. Scope switch navigates canonically. Members/channels allow reading for members; mutation controls honor role. Token nav/palette only owner/admin/developer; instance storage only instance admin.
- Storage page is instance-admin only; project prop is optional for /admin/storage. Legacy project storage retains ProjectRetention; full scope split awaits F3. Provider secret contents are never echoed back.
- In-app docs: all text lives in i18n `docs.articles[]` ({id,title,summary,sections[{title,paragraphs,code,language,steps}]}) in BOTH en.json and it.json (line ~680); search is client-side over that text. Must work with no project/admin/external service. Topic ids tested in App.test.tsx (`ingest-api`, `fivem`, unknown → not-found).
- Theming: `data-theme` on <html> (dark OLED default, light), CSS vars --bg/--surface/--ink*/--hue/--accent; use Tailwind color names (bg-surface, text-ink2, text-hue, border-line). Text tokens keep >=4.5:1 contrast. Breakpoint md = 721px (drawer below).
- i18n: every user-visible string in en.json + it.json (keep keys in sync); lang in localStorage `lang`.
- Touch targets `min-h-11`; status via role="status"/"alert" (tests and e2e query by role/label).
- Shell A (approved decision a6bd837dfa14): one 220px sidebar, workspace/role also in mobile topbar, real settings groups, focus-trapped mobile drawer. localStorage: workspace, project, theme, lang (old rail preference unused).

Gotchas:
- Live tail uses EventSource (GET only, cookie auth) and is disabled while attribute filters (`key=value`) are set, since the tail endpoint can't filter attributes; resumes from last cursor.
- vite `assetsInlineLimit` never inlines .woff/.woff2: server CSP has no font-src (falls back to 'self'); data: fonts would be blocked. Don't add external fonts/CDNs/inline scripts for the same CSP reason.
- Backend embeds `web/dist` (`backend/internal/webui/webui.go`, `go:embed all:dist`): run `npm run build` before building the Go binary or the UI is stale/missing. SPA fallback is handled server-side; only GET/HEAD allowed.
- Export can't detect truncation (HTTP trailers invisible to fetch).
- `searchLogs`/list helpers return `items: null` for empty; `list()` normalizes to []. Raw `request` callers must handle null.
- e2e is serial, single worker, creates the first admin; needs a real stack (no mocks), fails if env vars missing by design. Browser redesign QA fixtures intercept API calls; they do not prove real backend integration.
- package.json version (0.1.0) is not the release version (0.1.5 per git log); don't read it as such.
- Client still sends explicit ASCII slugs (lib/slug.ts); backend now derives a deterministic hash fallback for names with no ASCII. LogItem.tier labels archive rows; server level_counts ignores level filters, so chips use one search and filtered summaries select matching counts.
- API types are hand-written; settings, users and metadata use shared admin-client types, with account/email, health and admin-token APIs typed in web modules. Check backend contracts when extending coverage.
- OAuth sign-in is a browser navigation (not fetch) that returns via redirect.
- LogRows/LogDetails + lib/logPresentation: messages lead, date separators/relative time, actual Live/archive badges, seven level filters, scoped stream dropdown, grouped details with collapsed original. Actor/target shown only if present in attributes. No catalog/histogram API yet; no simulated production graph.
- query.ts quotes/backslashes round-trip saved/clicked filter values, including multiline values. Regression suites: Shell.test.tsx, pages/Logs.test.tsx, components/LogDetails.test.tsx, lib/query.test.ts.

Related: map/backend.md (API handlers, CSP, webui embed, auth/CSRF), map/sdk.md (ingest clients; docs articles describe them)
