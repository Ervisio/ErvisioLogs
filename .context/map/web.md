# web
Purpose: React19/Vite8/TS6 SPA served by Go embed; scoped log explorer + management, account, dashboards, docs en/it.
Commands in web: npm run dev | typecheck | lint | test | build | e2e (real disposable stack via scripts/e2e.sh).
Entry main.tsx (fonts/theme/i18n) → App.tsx (routes/auth/tenancy) → components/Shell.tsx (rail/project sidebar + outlet ShellContext).
Routes: /w/:workspaceId/p/:projectId[/streams/:id|alerts|dashboards|usage|settings/:tab], /w/:workspaceId/settings/:tab, /admin[/:section], /account; legacy /projects/:id links remain.
Auth public: /login, /forgot-password, /reset-password#token=, /verify-email#token=, /invite#token=.
lib/auth.tsx: only401 me=anonymous; network/5xx unreachable; CSRF module state via establish, safeNext guards redirects.
lib/tenancy.tsx: scoped URL workspace authoritative, localStorage fallback on unscoped routes; projects keyed workspace+attempt, stale responses abort.
API: lib/api.ts existing tenant/search/alerts/storage types + exported request; lib/manage-api.ts F2/F4 functions and injected shared admin-client; lib/notifications-api.ts F6 operations.
components/Management.tsx: scoped Page tabs/mobile selector, useRemote abort+identity guard, useAction ref prevents duplicate saves + stale completion; ActionForm disables pending fields.
pages/Settings.tsx: project/workspace rename/delete, metadata, settings source/env lock/inherit, scoped retention/purge; pages/catalog.tsx event catalog + field rules.
pages/Administration.tsx: health/users inline profile/session reset/admin tokens/audit/storage; instanceAdmin doesn't imply tenant membership; admin Workspaces metadata only.
pages/Account.tsx + lib/passkeys.ts: password/sessions/preferences, TOTP enrollment/recovery and browser WebAuthn JSON APIs; external-only accounts require fresh provider sign-in for security proof.
pages/AccountLink.tsx: email token fragments captured and scrubbed; invitation preview waits auth to avoid CSRF race. Login redirect uses #invite fragment, never token in query; external OAuth requires reopening original invite.
pages/Insights.tsx: dashboard count/series/top from saved query + /usage actual top-level events/bytes response; SystemAlerts requires admin+managed workspace.
Logs.tsx: pinned range anchor, abort guards after awaits, paged search + Live SSE, virtual table (42 desktop/54 mobile), level chips in Filters, catalog phrases/tags/actor chips.
LogEvent.tsx: safe template text, rule-hidden fields recursively pruned, raw JSON disclosure; Who/What filter paths explicitly attributes.*, builtins only supported fields clickable.
LogFilters.tsx: filter builder preserves quoted values, histogram /logs/histogram full aggregation + zero buckets/partial/brush; mobile filters scroll max40dvh.
query.ts: attr operators/exists plus tag:x; formatQuery optional tags; escaped quote/backslash values roundtrip.
Alerts/Channels/Users/Tokens/LogViews retain existing flows, now wizard/templates,email/Telegram/delivery log,invitations,expiry/rotation,update selected view. Embedded settings modes preserve parent tabs.
Theme: local Figtree + JetBrains Mono; data-theme dark|light|amoled persisted; --hue=text accent, --accent=fill. docs/DESIGN.md authoritative. Six blurred circles transform only, reduced motion/transparency respected.
Invariants:
- Every unsafe cookie request X-CSRF-Token + same-origin. Secrets never echoed after one-time creation; no browser token persistence.
- Scope all lists/permissions. Instance admin cannot read tenant logs. Menus hide inaccessible functions; no coming soon.
- All UI strings en.json+it.json synchronized; lang localStorage and html lang. Font files never inline (CSP self).
- Whole backend embeds built web/dist; build before backend binary. Docker/release source copies include packages/admin-client.
Verification: Vitest App, SecurityUI, Management, ManagementFlows, Flows, LogV2, query,slug; e2e/{fixture,first-run,redesign}.ts real stack. QA screenshots are /tmp artifacts, not production proof.
Related map/backend.md, map/sdk.md, map/integrations.md, docs/redesign/SECURITY-API.md.
