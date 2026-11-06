# Shared management client

`createAdminClient(request)` accepts the application's existing typed request function:
`<T>(method, path, body?, signal?) => Promise<T>`. Paths are relative to `/api/v1`; the injected
transport owns cookie/CSRF or socket authentication and error handling. This keeps web and plugin
on one management contract without granting operators access to log data.

`createFetchRequest({ baseUrl, token })` is the remote Bearer adapter. It omits credentials, applies
a 30-second timeout and decodes RFC7807 errors through `AdminError`. The plugin approves the HTTPS
host through its SDK before using this adapter. Session callers can supply a `csrfToken` callback.

Run `npm test` with Node 24 or newer. Source is TypeScript and consumed directly by each Vite build.
