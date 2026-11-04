# ADR 0019: Local admin socket and instance admin tokens

Status: Proposed. Written for 0.2. Checked with `go test`, `go vet`, `make openapi-lint` and the API integration tests;
the socket was not tried in a running container.

## Context
The Ervisio plugin should manage the instance: users, settings, storage, workspaces, projects and their tokens. It
cannot call the API as it is. Its page runs in a sandboxed iframe without cookies, so a session is out, and its SDK
reaches the host over HTTP only through a unix socket. API tokens are bound to one project and cannot touch tenancy.
Instance administration was tied to a user session, and being an instance administrator never gave access to a
workspace's logs. That must stay true for anything new.

## Decision
A third kind of principal, the operator, sits beside users and API tokens. It is not a user and has no membership.
An operator is either the local admin socket or an instance admin token.

- Scopes: `admin:users`, `admin:settings`, `admin:storage` (storage and retention), `admin:tenancy` (workspaces,
  projects, members, invitations, their settings and API tokens) and `admin:read`, which allows every safe method of
  the others. A missing scope answers 403.
- In every existing workspace an operator acts as an owner holding only `logs:ingest`, `projects:write`,
  `tokens:manage`, `members:manage` and `workspace:manage`. The log routes, saved queries and alerts accept only
  users and API tokens, so an operator gets 401 there. An API token it issues has no creator, is marked
  `issued_by_operator` and can only ingest: asking for `logs:read` or `logs:export` answers 403, and the store drops
  any other scope at lookup.
- A workspace created by an operator needs `owner_id`, an active user who becomes its owner. A session sending
  `owner_id` gets 422. Users who are not members still get 404.
- The socket (`ERVISIO_LOGS_ADMIN_SOCKET`, empty disables it) serves the whole API through a marker middleware and
  holds every admin scope. Reaching the file is the authorization, as with `docker.sock`: no session, no CSRF. It is
  created with mode 0660; a stale socket from a crashed process is removed at start, and a live one or any other file
  is refused. Compose puts it in the `ervisiologs_admin` volume, enabled by default; the systemd unit puts it in
  `/run/ervisio-logs`, a 0750 directory of the service group.
- An admin token (`evl_adm_...`, migration 0009) is created, listed and revoked only from an instance administrator's
  session, under `/admin/tokens`. Expiry is required and at most one year away. Only its HMAC is stored. It is
  accepted only in the `Authorization: Bearer` header, never as a cookie, and stops working at once when it is
  revoked or expired, or when its creator is disabled, deleted or no longer an instance administrator.
- Audit events keep the operator in `actor_name`: the token name, or `plugin` for the socket. `actor_token_id`
  holds the admin token's id.
- The admin-token routes (`/admin/*`, `/workspaces*`, `/projects*`, `/tokens*`) answer CORS with
  `Access-Control-Allow-Origin: *` and never allow credentials, and answer preflight `OPTIONS` with 204. Bearer
  requests skip the Origin and CSRF checks, as API tokens already did; cookie requests keep both.

## Risks
- An operator can delete workspaces and projects, which deletes their logs, and can change retention. "No log access"
  means no reading, exporting or deleting of single events, not that the data is safe from it.
- An operator can add any user, a user it controls included, to a workspace, or reset passwords with `admin:users`,
  and so reach logs through that user. The audit trail records it with the operator's name.
- Go creates the socket with the process umask before the chmod to 0660: the directory around it must not be
  world-accessible. Anyone in the socket's group, or who can mount the Compose volume, is a full administrator.
- `actor_token_id` alone does not say whether an API token or an admin token acted; `actor_name` does.
- An API token issued by an operator outlives the admin token that issued it; it can only ingest.
- With CORS open to every origin, a leaked admin token can be used from any page until it expires or is revoked.
