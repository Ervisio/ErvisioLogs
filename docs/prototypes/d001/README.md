# D001 prototype: remote Parquet query engine

This prototype measures ClickHouse table functions, DuckDB and DataFusion on archived Parquet segments stored on S3 and
on SFTP. It answers ADR 0002 (`docs/adr/0002-remote-parquet-query-engine.md`).

## Run

```sh
docs/prototypes/d001/run.sh                         # 10M events, 3 cold runs, 25 ms per S3 request
EVENTS=200000 RUNS=1 docs/prototypes/d001/run.sh    # smoke run, about 3 minutes
ONLY_ROBUSTNESS=1 EVENTS=200000 docs/prototypes/d001/run.sh
```

Everything runs inside Docker in the Compose project `ervlogs-d001`. No port is published to the host. The script
generates random S3 and ClickHouse credentials plus SSH keys for each run, never prints them, and removes the
containers and volumes at the end. Results land in `results/`.

## What runs

| service | role |
|---|---|
| `s3` | SeaweedFS S3 gateway (the MinIO images could not be pulled here) |
| `sftp` | OpenSSH with key auth and a pinned host key |
| `clickhouse` | the same ClickHouse image as the product |
| `bench` | generator, counting S3 proxy, benchmark driver, DuckDB 2.0.0, DataFusion 55.0.0 (Python packages) |

`gen.py` builds one project-day of synthetic events, sorted by time, and writes the same rows in three layouts:
one file for the day, 24 hourly files and 288 five-minute files. It uploads each file to S3 (size checked after upload)
and to SFTP (temporary name, size check, then rename). It also computes the expected answer of every query with
pyarrow, so each engine's result is checked, not only timed.

`bench.py` plans files from the manifest the way the catalog will: by project and time overlap. It never lists a
directory. Per layout and query:

- **S3**: each engine reads the planned objects through `proxy.py`, which adds `LATENCY_MS` to each request and
  counts requests and bytes.
- **SFTP**: none of the three engines reads SFTP, so the planned files are downloaded into the cache first. The
  download is timed once per run, then each engine queries the local copies.

DuckDB and DataFusion run in a new process per query, so every run is cold, and peak memory is the process peak RSS
minus a process that only imports both libraries. ClickHouse runs with its query, condition, schema and page caches
off. Its peak memory comes from `system.query_log`.

`robustness.py` feeds each engine a truncated file, random bytes, a footer with a wrong length, an empty file and a
valid file for another project.

## Queries

| id | what | window |
|---|---|---|
| q1_needle | case-insensitive text search, newest 100 | one hour |
| q2_attr | `attributes.user = 'ada'`, newest 100 | whole day |
| q3_levels | count per severity | whole day |
| q4_page | newest 100 events (first Explorer page) | one hour |

## Limits

- One machine: S3 and SFTP run next to the engines. The S3 numbers include the injected latency. The SFTP numbers
  have none, so a real Storage Box over the internet will be slower than shown.
- DataFusion's Python package has no JSON functions, so its q2 uses a substring match on the JSON text. The Rust crate
  has `datafusion-functions-json`; this prototype does not test it.
- DataFusion registers one table per planned file and unions them, so each file costs a schema fetch before the query
  starts. A listing table over a directory would avoid that, but would let the directory rather than the catalog
  decide which files are read.
- One dataset size (10M events). ADR 0002 also asked for 100M events and first-row streaming latency; this run does
  not measure them.
