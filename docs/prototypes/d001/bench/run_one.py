"""Runs one query with DuckDB or DataFusion in a fresh process, so every run starts cold and ru_maxrss is the peak
resident memory of that run alone. Prints one JSON line.

usage: run_one.py <duckdb|datafusion> <s3|local|baseline> <query> <keys.json>
With source "baseline" it only imports the engine, which gives the RSS to subtract.
"""
import json
import os
import resource
import sys
import time

import common as c
import queries as q


def duckdb_run(source: str, query: str, keys: list[str]):
    import duckdb

    con = duckdb.connect()
    if source == "s3":
        con.execute("LOAD httpfs")
        con.execute(
            "CREATE SECRET s3 (TYPE s3, KEY_ID ?, SECRET ?, ENDPOINT 'localhost:9000', URL_STYLE 'path', "
            "USE_SSL false, REGION 'us-east-1')".replace("?", "'{}'").format(os.environ["S3_ACCESS_KEY"], os.environ["S3_SECRET_KEY"])
        )
        paths = [f"s3://{c.BUCKET}/{k}" for k in keys]
    else:
        paths = [f"/cache/{k}" for k in keys]
    src = "read_parquet([" + ",".join(f"'{p}'" for p in paths) + "])"
    return con.execute(q.sql("duckdb", query, src)).fetchall()


def datafusion_run(source: str, query: str, keys: list[str]):
    from datafusion import SessionContext
    from datafusion.object_store import AmazonS3

    ctx = SessionContext()
    if source == "s3":
        store = AmazonS3(bucket_name=c.BUCKET, region="us-east-1", access_key_id=os.environ["S3_ACCESS_KEY"],
                         secret_access_key=os.environ["S3_SECRET_KEY"], endpoint="http://localhost:9000", allow_http=True)
        ctx.register_object_store("s3://", store, c.BUCKET)
        paths = [f"s3://{c.BUCKET}/{k}" for k in keys]
    else:
        paths = [f"/cache/{k}" for k in keys]
    # One registered table per planned file: the catalog, not a directory listing, decides what to read.
    for i, p in enumerate(paths):
        ctx.register_parquet(f"f{i}", p)
    src = "(" + " UNION ALL ".join(f"SELECT * FROM f{i}" for i in range(len(paths))) + ") AS ev"
    return [tuple(r.values()) for r in ctx.sql(q.sql("datafusion", query, src)).to_pylist()]


def main() -> None:
    engine, source, query, keys_file = sys.argv[1:5]
    keys = [] if source == "baseline" else json.load(open(keys_file))
    start = time.perf_counter()
    rows, error = [], None
    try:
        if source == "baseline":
            __import__(engine)
        elif engine == "duckdb":
            rows = duckdb_run(source, query, keys)
        elif engine == "datafusion":
            rows = datafusion_run(source, query, keys)
    except Exception as e:  # reported, never hidden
        error = f"{type(e).__name__}: {e}"[:300]
    wall = time.perf_counter() - start
    print(json.dumps({
        "rows": len(rows), "digest": q.result_digest(query, rows) if rows else None, "wall_s": round(wall, 3),
        "peak_rss_mb": round(resource.getrusage(resource.RUSAGE_SELF).ru_maxrss / 1024, 1), "error": error,
    }))


if __name__ == "__main__":
    main()
