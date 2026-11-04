# Ervisio Logs 0.2 design brief

Status: F0 visual choices approved on November 4, 2026. Shell A, settings A, users remix A, plugin B; no notes. Scope: phase F0 of [PLAN.md](PLAN.md).

## Brief

Product-app for game-server operators and developers; desktop and mobile; existing fidelity. Keep Ervisio's OLED black, amber, Figtree, and JetBrains Mono for technical values. The [November 2 decisions](../../ervisio-logs-spec/design/UI_DECISIONS.md) remain the baseline; this round proposes navigation changes for 0.2.

Material: opaque panels with hairline separators. Touch controls reach 44px; mobile navigation and detail use drawers. Review at 1280×800, 1440×900, and 390×844. Compare shell, rows, and detail first. Settings, instance users, and plugin require later rounds; this round alone does not complete F0.

## Reference observations

Reviewed November 4, 2026:

- [Grafana logs](https://grafana.com/docs/grafana/latest/visualizations/panels-visualizations/visualizations/logs/): timestamp, severity, and message lead each row; expandable details keep fields near the event. Reuse progressive disclosure.
- [Better Stack live tail](https://betterstack.com/community/blog/live-tail-presets/): a histogram exposes log volume changes; presets retain recurring filters. Reuse the adjacent time overview and saved views.
- [Axiom query builder](https://axiom.co/docs/query-data/explore): field, operator, and value form a filter; the time range belongs to the query. Reuse that sequence for routine filtering.
- [Grafana server users](https://grafana.com/docs/grafana/latest/administration/user-management/server-user-management/) separates instance user management from organization membership. [Axiom organization settings](https://axiom.co/docs/reference/organization-settings) groups settings under the selected organization. Reuse scoped destinations and a separate user list; Ervisio's own backend defines permissions and available actions.
- Local approved explorer/navigation screenshots define the existing brand and project/channel model. Their simulated data does not prove API behavior or accessibility.

External references guide interaction hierarchy; Ervisio's tokens and components define appearance.

## Token lock before markup

Reuse `web/src/index.css`; production tokens stay unchanged until approval.

| Role | Dark | Light |
| --- | --- | --- |
| Canvas | `#000000` | `#ececef` |
| Surface | `#0e0e10` | `#f7f7f9` |
| Inset | `#18181b` | `#ffffff` |
| Raised / divider | `#26262b` / `#232327` | `#dcdce2` |
| Primary text | `#f4f4f6` | `#16161a` |
| Secondary / muted text | `#b0b0ba` / `#8f8f9a` | `#50505b` / `#5f5f6a` |
| Accent text | `#ffb547` | `#865000` |
| Accent fill / text | `#ffb547` / `#1c1100` | same |
| Success | `#3ddc97` | `#0f7a50` |
| Warning | `#f2c94c` | `#7a5600` |
| Error | `#ff6b81` | `#c42d4a` |

Self-hosted Figtree: 14px/500. JetBrains Mono: 12px for technical values. Spacing: 4/8/12/16/24px. Radius: 7px for rows and controls. Controls: 36px desktop, 44px touch. Rows: 48px compact, 64px readable. Focus: 2px amber outline, 2px offset. Motion: 100ms ease-out; respect reduced motion. Use surface separation and borders without decorative shadows.

## Directions

| Variant | Structure | Tradeoff |
| --- | --- | --- |
| A · Navigazione unificata | Workspace/project sidebar; wide results; optional filters; right detail | Less navigation overhead; open the panel to filter |
| B · Banco di ricerca | Compact project rail; persistent stream/level filters; dense rows; right detail | Faster repeated filtering; less horizontal room |
| C · Registro leggibile | Named project sidebar; two-line event rows; inline detail | Easier reading and mobile continuity; fewer events per screen |

All show workspace and role, project, environment, time range, seven levels, saved views, and a Live toggle. Settings have workspace/project/instance entry points. Instance administration requires an instance admin. Scoped URLs follow `/w/:workspace/p/:project/...`. Future destinations stay absent until their functions exist.

## Fidelity and interaction

Reuse the simulated Roma RP events from `ervisio-logs-spec/design/approved/navigation/prototype.html` and actual FiveM schemas from `integrations/fivem/README.md` and `examples/esx/server.lua`. Preserve technical names, messages, resources, and attributes under “Mostra originale”. Italian titles and grouped details illustrate the proposed F4 presentation, not fields returned by today's backend. Label sample data in the gallery context.

Preview controls work locally: search, stream/level filters, live switch, detail open/close, scope settings, mobile navigation. Previews never write to the backend. Counts derive from sample rows; archive labels follow each row's source. Avoid invented production statistics or connectivity claims.

Review Italian and English labels with stop-slop. Use task names: “Log”, “Ambienti”, “Membri e inviti”, “Conservazione”, “Vai a…”. Avoid promotional copy and “coming soon”.

## Acceptance and evidence

- Check desktop/mobile, dark/light, focus, detail dismissal, filters, long messages, and empty results.
- Measure contrast, overflow, and touch targets; inspect screenshots.
- Record the owner's decision before porting layout changes into React.

Gallery: `.variant-studio/rounds/001-shell-explorer/` (local, ignored by Git). Selected: A, Navigazione unificata. The owner reviewed it in light theme at laptop width; the target remains laptop and mobile with both themes. [Approved snapshot](approved/shell/prototype.html), [screenshot](approved/shell/screenshot.png), and [decision](approved/shell/decision.json) retain the review evidence.

The React port uses existing API fields and actions. Event catalog titles, tag support and the real histogram require F4; do not substitute sample catalog values or a simulated graph in the application.

### Preview QA on November 4

Playwright with installed Chrome 156 checked all three variants at 1440×900, 1280×800, and 390×844 in dark and light themes: 18 combinations, no JavaScript/HTTP errors or document overflow. Visible mobile controls measured at least 44px in both dimensions. Root inspected screenshots of the desktop/light/mobile layouts and long details; detail and dialog headers retain their close control while scrolling.

At 1280px and 390px, all three variants passed search/empty state, seven level choices with stable counts, stream selection, save/restore of a local view, Live toggle, original JSON, scope dialogs, and dialog keyboard containment. The histogram shows sample counts and is passive; the range select changes the interval. Preview views last for the open page only; Live does not connect to the instance.

Sampled body/muted text contrast meets 4.5:1 on the checked surfaces (muted dark on raised 4.71:1; muted light on raised 4.62:1). Severity colors on raised surfaces require care when porting: the existing light success/error tokens fall below 4.5:1 there; use those combinations for non-text marks only. This sample check is not a full accessibility audit.

Local evidence: `_qa/layout.json`, `_qa/interactions.json`, `_qa/contrast.json`, and PNG captures beside the round. This proves preview behavior only; production backend integration and F3 implementation remain outside this evidence.

### React port

Implemented the selected sidebar and log presentation with existing React components and API actions. Canonical URLs carry workspace and project; old project links resolve and redirect. Unknown/mismatched workspace links stay isolated. Workspace failures retain their scope on the retry page. Navigation omits unavailable pages; token links and quick commands require owner/admin/developer. Instance storage works without a project.

Messages lead each row; technical event names remain secondary until the catalog exists. The right detail uses real attributes, groups available actor/target fields, and keeps original JSON collapsed. All seven translated levels remain available with server counts. Saved/clicked filters retain quotes, backslashes, and multiline values. Existing SSE, export, saved views, pagination and first-event actions remain wired.

Playwright exercised the React app at 1440px desktop and 390px mobile, both themes, plus old links, using intercepted API fixtures. No JavaScript errors, unexpected API calls, or horizontal overflow. Browser checks covered search, stable level counts, scope switching, detail JSON, mobile controls and keyboard drawer dismissal. Evidence: `.variant-studio/prod-qa/captures/` (ignored by Git). These simulated responses do not prove Postgres, ClickHouse, archive or real SSE integration.

No production histogram or sample catalog titles were added. The management choices below have been ported; the remaining F3/F4 functionality is listed in PLAN.md.

## Management comparisons

Keep the approved shell, palette, typography and control sizes. The following local rounds compare organization and interaction, rather than introduce another visual identity:

| Round | A | B | API constraints |
| --- | --- | --- | --- |
| 002 · Settings | Section navigation with grouped fields | Scope header with expandable setting groups | Real key schema; env overrides lock; show DB/default/inherited sources only where that scope permits them |
| 003 · Instance users | Searchable list with right inspector | Readable list with focused action dialogs | Separate instance administration and membership; protect self, last active admin and last workspace owner |
| 004 · Plugin | Task tabs in the host kit | Grouped internal navigation | Preserve SDK patterns; distinguish runtime settings, local socket/remote admin token, and infrastructure env changes |

All changes in these previews affect fixtures within the open page. Keep existing plugin setup/address/console patterns and real backend fields. Never read actual secrets to populate a design.

### Approved management choices

| Surface | Choice | Decision | Evidence |
| --- | --- | --- | --- |
| Settings | 002 A, section navigation | `2b78002d7bb2` | [Prototype](approved/settings/prototype.html), [decision](approved/settings/decision.json) |
| Instance users | 003 remix → 005 A, readable list and right inspector | `b40fdd483bc3` | [Prototype](approved/instance-users/prototype.html), [decision](approved/instance-users/decision.json) |
| Plugin | 004 B, grouped internal navigation | `befd1a6aae91` | [Prototype](approved/plugin/prototype.html), [decision](approved/plugin/decision.json) |

Root browser layout checks passed for all directions at 1280px and 390px, both themes, with no final JavaScript/HTTP errors, horizontal overflow, or undersized mobile controls. User management interactions passed search, pagination, self/owner protections, local creation and disable on both initial directions and the remix. Settings and plugin agents also recorded their interactive checks. Missing-script errors during generation preceded the completed previews; selected screenshots show the rendered designs. No management preview connected to an instance.

The installed plugin SDK 0.3.0 has no secret vault API. HTTPS management will keep an admin token only in page memory, with an explicit session-lifetime notice. Persistent protected storage requires SDK support; plaintext files or browser storage are not a substitute. The shared admin client owns typed endpoint calls, while each app supplies its transport and authorization.

### Management implementation and evidence

The React settings pages use the backend schema at instance, workspace and project scope. Environment locks, inherited sources and read-only roles remain visible. Saves acknowledge each successful key and retain failed edits for retry; no bulk transaction is implied. Project environment and stream metadata use their real endpoints. Instance users use the approved readable list and inspector, with search, pagination, protected self actions, sessions and typed deletion confirmation.

Account pages cover password changes and sessions. Public email pages cover password recovery, reset, verification and invitation acceptance or registration. Link tokens are removed from the fragment, held in page memory and submitted only in POST bodies. Invitation continuation through local sign-in uses a token-free URL; a page refresh or external-provider round trip requires opening the email link again. Public actions wait for session bootstrap, so an existing cookie can supply its CSRF token. Late bootstrap responses cannot replace a newly established session.

Instance overview reads actual health fields, marks stale snapshots and suppresses connection error details. Admin tokens have five explicit scopes, a required expiry, confirmed revocation and a masked one-time value. The list reports stored revocation/expiry without assuming that the creator still has administrator access.

The plugin uses grouped navigation and the SDK kit, with a shared typed client for real management endpoints. Local transport limits the available socket paths and separates queries from paths; remote HTTPS omits cookies and rejects redirects. Setup, infrastructure environment editing, address management and bounded job history remain available. No host service was started, restarted or deployed during this work.

Browser QA with intercepted APIs passed 28 settings/user checks, 28 account/email checks and 16 instance overview/token checks across desktop/mobile and both themes. Root inspected representative screenshots of settings, users, account, invitations, overview and token creation. Reports and captures are local under `.variant-studio/prod-qa/{management,account,instance-admin}/`. These checks cover the recorded interactions, overflow, JavaScript errors and unexpected API calls; they do not measure all focus/target behavior, clipboard operation, SMTP delivery or actual backend integration. The plugin's built bundle was exercised with a mocked SDK; actual Ervisio host/socket permissions remain unverified.
