# Redesign management milestone handoff
Snapshot before the user-authorized commit and local main update. Overall redesign remains incomplete.
Goal: Continue redesign 0.2 from the F2 handoff. Local F0/F1 followups, F3 management and F5 plugin milestone implemented; overall plan remains incomplete.
Branch: feat/redesign-f0-followups. No commit, push, merge, release or deploy. Preserve pre-existing INDEX.md/config edits.
User: Italian collaboration; multi-agent explicitly requested. Mechanical work gpt-6.1-terra; complex work gpt-6.1-sol (overrides configured keeper model).
Done:
- F2a-f/F1 verified on starting main dcd3bdc; old handoff was stale. Archive: archive/2026-11-04-f2-backend.md.
- Backend slug fallback for non-ASCII names; counts ignore level filters; actual hot/archive source per row/export/tail. Web uses one search and preserves filtered summary/Live evidence.
- F0 approvals: shell001A a6bd837dfa14; settings002A 2b78002d7bb2; users003 remix->005A b40fdd483bc3; plugin004B befd1a6aae91. No notes. Standalone HTML/decisions/screenshots in docs/redesign/approved/.
- React unified shell, canonical scoped URLs and old-link redirects, isolation/stale response guards, mobile drawer, existing real log actions, seven levels, grouped details/JSON, quote-safe query parser.
- Settings three scopes with real schema/sources/env locks, role permissions and per-key partial-save recovery; editable stream/environment metadata.
- Admin users list/search/cursor/inspector/create/status/admin/password/sessions/typed deletion; own actions protected and server conflicts visible.
- Account profile/password/sessions; forgot/reset/verify/invitation preview+accept+registration. Fragment removed, token only POST+SPA memory; local sign-in continuation token-free, refresh/external OAuth requires reopening link.
- Auth bootstrap aborts on establish/logout/retry, ignores obsolete responses. Public email actions wait for resolved session/CSRF; deferred-bootstrap regression tests pass.
- /admin health polling/stale markers/redacted connection errors; /admin/tokens five scopes/required expiry/confirmed revoke/masked one-time secret.
- Plugin grouped B management UI, shared packages/admin-client, socket allowlists and HTTPS, existing setup/env/Caddy, bounded job history. No host services changed.
SDK constraint: plugin-sdk0.3.0 has no vault. HTTPS token held only in open-page memory, cleared on disconnect/target change/unmount. Never settings/files/localStorage/sessionStorage/logs. Protected persistence awaits SDK support; user informed.
Validation:
- API unit/vet and OpenAPI pass (10 existing lint warnings); integration tests compile, not run against real DBs. Full Go suite has Windows TestListenUnix permission mismatch (0666 vs0660).
- Web build/typecheck/lint and all150 unit tests (17 files, one worker) pass. Build warns main bundle >500kB. Fresh embedded backend build passes at .dev/redesign-review/ervisio-logs.exe.
- Management browser mock QA 72/72: settings/users28 + account/email28 + health/admin-tokens16, desktop/mobile both themes; root inspected representative screenshots. No unexpected APIs/JS errors/overflow; account rerun after CSRF fix also28/28. No actual SMTP/DB/clipboard/host proof; full focus/target coverage not measured.
- Earlier shell mock QA and all approved preview layouts/interactions pass. Plugin17 tests +shared4 +built-bundle mock-SDK1 passed; final plugin build passes after one Italian accent correction.
Next: Finish F3: rename/delete workspace/project, create/delete environment/stream, outgoing invitations list/create/revoke, split Archive/Retention and purge. Current retention API is instance-admin-only; design tenant authorization rather than granting implicit instance privileges.
After F3: F4 real tags/catalog/histogram/tail attribute filtering; F6 alerts/channels; F7 dashboard/usage/audit; F8 SDK/FiveM; F9 optional TOTP/passkeys/security. See docs/redesign/PLAN.md and DESIGN.md. Do not claim whole 0.2 complete.
Ownership: all agents finished their assigned files; no active implementation overlap. Root controls integration/checks/docs. Keeper updated map/web.md and map/integrations.md only.
Local previews: Vite127.0.0.1:5175 session98095; Variant Studio127.0.0.1:17849 (key .variant-studio/state/server.json). All design decisions consumed. Mock QA harness/reports under .variant-studio/prod-qa/ (ignored).
