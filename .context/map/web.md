# web
Purpose: React SPA for Ervisio Logs (sign-in, workspaces/projects, log explorer + live tail, alerts, channels, storage admin, tokens, members, bundled docs), served by the Go backend from embedded `web/dist`.

Stack: Vite 8, React 19, react-router-dom 7 (BrowserRouter), TypeScript 6, Tailwind 4 (`@tailwindcss/vite`, tokens in `src/index.css`), i18next (en/it), lucide-react, Vitest+jsdom+RTL, Playwright. No data/query lib: plain `useEffect` + `AbortController`.

Commands (cd web): `npm run dev` (proxies /api,/healthz,/readyz to `VITE_BACKEND` or 127.0.0.1:8080) | `build` (tsc -b && vite build) | `typecheck` | `lint` | `test` (vitest run) | `e2e` (needs E2E_BASE_URL + E2E_SETUP_CODE, run via `scripts/e2e.sh`).

Entry points: `src/main.tsx` (initTheme, i18n, fonts) → `src/App.tsx:App` (routes) | `src/lib/api.ts:api,request,ApiError,setCsrfToken,tailUrl` | `lib/auth.tsx:AuthProvider,RequireAuth,safeNext` | `lib/tenancy.tsx:TenancyProvider,useTenancy` | `lib/sections.ts:SECTIONS` | `components/Shell.tsx` (rail/nav/topbar via outlet ctx `ShellContext.topbar`) | `lib/query.ts` (search-box parser) | `lib/useReadiness.ts`.

Flow:
- `/login` → pages/Login (local + OAuth providers, first-admin setup w/ setup_code) → api.providers/login/register → /api/v1/auth/*
- RequireAuth (api.me → /auth/me) → TenancyProvider (api.projects?workspace_id=) → Shell
- `/` Home (last project from localStorage `project`) | `/projects` | `/projects/:projectId[/streams/:streamId]` Explorer → pages/Logs → POST /logs/search (cursor paging, include_total)
- `/projects/:id/:section` SectionPage (pages/Workspace.tsx) dispatches: live→Logs live (EventSource GET /logs/tail), alerts→Alerts (/alerts, /queries), integrations→Channels (/notification-channels), storage→Storage (/admin/storage, /admin/retention), tokens→Tokens (/tokens), users→Users (/workspaces/:id/members). Other SECTIONS (dashboards, usage, audit, settings) render a "coming soon" Notice.
- `/status` Overview (useReadiness polls /readyz every 10s) | `/docs[/:topic]` Documentation | `*` NotFound
- Export: api.exportLogs → POST /logs/export (blob; ndjson|csv)

Invariants:
- Backend contract: `/api/v1`, cookie session (`credentials: same-origin`), `X-CSRF-Token` on every non-GET (token from Session.csrf_token, held in module var, cleared on logout/401), errors are RFC 7807 (`detail`) → ApiError(status, retryAfter).
- Auth: only 401 from /auth/me means anonymous; 5xx/network → "unreachable" screen with retry (never bounce to login). Any 401 in tenancy calls auth.retry(). `?next=` goes through `safeNext` (same-app paths only; rejects `//`, `/\`).
- Workspace scoping: current workspace = localStorage `workspace` (fallback first of session.workspaces). Project lists are keyed `${workspaceId}:${attempt}` so a stale response never shows after switching. Pages take `key={project.id}`/`key={workspace.id}` to reset state on switch. Every list call carries workspace_id/project_id; alerts/channels/members are workspace-role gated (Users: MANAGERS set; owner-only actions).
- Storage page is instance-admin only (`user.is_instance_admin`); non-admin gets forbidden notice. Provider secrets are typed in-UI (`putSecret`), only referenced by `secret_file` name; never echoed back.
- In-app docs: all text lives in i18n `docs.articles[]` ({id,title,summary,sections[{title,paragraphs,code,language,steps}]}) in BOTH en.json and it.json (line ~680); search is client-side over that text. Must work with no project/admin/external service. Topic ids tested in App.test.tsx (`ingest-api`, `fivem`, unknown → not-found).
- Theming: `data-theme` on <html> (dark OLED default, light), CSS vars --bg/--surface/--ink*/--hue/--accent; use Tailwind color names (bg-surface, text-ink2, text-hue, border-line). Text tokens keep >=4.5:1 contrast. Breakpoint md = 721px (drawer below).
- i18n: every user-visible string in en.json + it.json (keep keys in sync); lang in localStorage `lang`.
- Touch targets `min-h-11`; status via role="status"/"alert" (tests and e2e query by role/label).
- localStorage keys: workspace, project, rail, theme, lang.

Gotchas:
- Live tail uses EventSource (GET only, cookie auth) and is disabled while attribute filters (`key=value`) are set, since the tail endpoint can't filter attributes; resumes from last cursor.
- vite `assetsInlineLimit` never inlines .woff/.woff2: server CSP has no font-src (falls back to 'self'); data: fonts would be blocked. Don't add external fonts/CDNs/inline scripts for the same CSP reason.
- Backend embeds `web/dist` (`backend/internal/webui/webui.go`, `go:embed all:dist`): run `npm run build` before building the Go binary or the UI is stale/missing. SPA fallback is handled server-side; only GET/HEAD allowed.
- Export can't detect truncation (HTTP trailers invisible to fetch).
- `searchLogs`/list helpers return `items: null` for empty; `list()` normalizes to []. Raw `request` callers must handle null.
- e2e is serial, single worker, creates the first admin; needs a real stack (no mocks), fails if env vars missing by design. Unit tests mock fetch (App.test.tsx, ~730 lines).
- package.json version (0.1.0) is not the release version (0.1.5 per git log); don't read it as such.
- OAuth sign-in is a browser navigation (not fetch) that returns via redirect.

Related: map/backend.md (API handlers, CSP, webui embed, auth/CSRF), map/sdk.md (ingest clients; docs articles describe them)
