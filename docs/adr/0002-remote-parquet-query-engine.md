# ADR 0002: Remote Parquet query engine (D001)

Status: Proposed. No prototype has been run. Nothing below is a measured result.

## Context
Archived events are immutable Parquet segments on S3 or on a Hetzner Storage Box over SFTP. Queries must filter by
tenant/project/stream/time and by common attributes, stream results, and stay inside memory budgets.
SFTP has no range reads or server-side filtering we can rely on, so it needs staging into the local cache.

## Options
1. **ClickHouse table functions** (`s3`, `file` over a cache directory). Same SQL dialect as the hot tier; the planner already
   exists. Open question: SFTP is not a native source, so segments must be downloaded first, then read with `file()`.
2. **DuckDB** embedded in the Go process (or a sidecar). Strong Parquet pushdown, httpfs for S3. Adds a CGO or sidecar dependency.
3. **Apache DataFusion** (Rust sidecar). Fast, but a second language and runtime to operate.

## Prototype plan (to run before deciding)
Dataset: 10M and 100M synthetic events with realistic JSON attributes, segment sizes 32, 64, 128, 256 MB.
Measure per engine and per source (local, S3-compatible such as MinIO, SFTP staged):
selective query latency (project + 1h window + severity), full-scan time, peak RSS, bytes fetched, file count sensitivity,
streaming first-row latency, behaviour on a truncated or malicious Parquet file.
Pass criteria: bounded memory under a configured limit, first rows before full scan, no tenant data crossing in one query.

## Working assumption
Plan on option 1 for S3 and for staged SFTP files because it adds no new runtime. The assumption is untested.

## Risks
Attribute filters on JSON columns may defeat row-group pruning. Many small files hurt every engine.
