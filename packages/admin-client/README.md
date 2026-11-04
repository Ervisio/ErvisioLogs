# Ervisio Logs admin client

`createAdminClient(request)` accepts a generic transport with the same `(method, path, body?, signal?)` signature as
the web API request helper. Paths are relative to `/api/v1`; the transport owns the base URL, serialization,
authentication, cancellation and error conversion. The client never stores credentials or imports a UI framework.

Settings preserve typed values, source, inherited value and server locks. User, tenancy, metadata, ingest-token,
health, storage, retention and audit methods follow the existing operator API. Errors propagate unchanged so each
UI can display its own permission and conflict states. Workspace/project deletion requires a confirmation slug.

Run `node --test test/*.test.ts` in this directory. No build or dependencies are required.
