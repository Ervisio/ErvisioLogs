"""What each engine does with a damaged segment: truncated footer, garbage bytes, a valid file whose schema lies about
the project, and a footer that claims a huge row count. The answer we need is "fails with an error, does not hang,
does not crash the server, does not return rows". Writes /results/robustness.json and appends to results.md.
"""
import json
import os
import shutil
import subprocess
import time

import clickhouse_connect
import pyarrow as pa
import pyarrow.parquet as pq

import common as c

DIR = f"{c.PROJECT}/{c.DAY}/bad"


def cases() -> dict[str, bytes]:
    src = open(f"/data/segments/{c.segment_key('hour', 13)}", "rb").read()
    other = pa.table({"project_id": ["someone-else"], "event_id": ["x"], "timestamp": pa.array([0], pa.timestamp("ms", tz="UTC")),
                      "message": ["leak"]})
    sink = pa.BufferOutputStream()
    pq.write_table(other, sink)
    # A footer length that points before the start of the file.
    lying_footer = src[:-8] + (len(src) * 4).to_bytes(4, "little") + b"PAR1"
    return {
        "truncated": src[: len(src) // 2],
        "garbage": os.urandom(4096),
        "footer_lies": lying_footer,
        "empty": b"",
        "other_project": sink.getvalue().to_pybytes(),
    }


SQL = f"SELECT count(*) FROM {{src}} WHERE project_id = '{c.PROJECT}'"


DUCK = "import duckdb, sys; print(duckdb.connect().execute(sys.argv[2].format(src=f\"read_parquet('{sys.argv[1]}')\")).fetchall())"
DF = ("import sys; from datafusion import SessionContext; ctx = SessionContext(); ctx.register_parquet('t', sys.argv[1]); "
      "print(ctx.sql(sys.argv[2].format(src='t')).to_pylist())")


def run_duck(path: str) -> str:
    return _sub(DUCK, path)


def run_df(path: str) -> str:
    return _sub(DF, path)


def _sub(code: str, path: str) -> str:
    try:
        out = subprocess.run(["python", "-c", code, path, SQL], capture_output=True, text=True, timeout=60)
    except subprocess.TimeoutExpired:
        return "HANG (killed after 60 s)"
    if out.returncode == 0:
        return "rows: " + out.stdout.strip()
    if out.returncode < 0:
        return f"CRASH (signal {-out.returncode})"
    lines = [l.strip() for l in out.stderr.splitlines() if "Error" in l or "Exception" in l] or ["?"]
    return "error: " + lines[-1][:200]


def main() -> None:
    ch = clickhouse_connect.get_client(host="clickhouse", username="bench", password=os.environ["CLICKHOUSE_PASSWORD"])
    shutil.rmtree(f"/cache/{c.PROJECT}", ignore_errors=True)
    os.makedirs(f"/cache/{DIR}", exist_ok=True)
    results = {}
    for name, data in cases().items():
        path = f"/cache/{DIR}/{name}.parquet"
        open(path, "wb").write(data)
        row = {}
        start = time.perf_counter()
        try:
            r = ch.query(SQL.format(src=f"file('cache/{DIR}/{name}.parquet', 'Parquet')")).result_rows
            row["clickhouse"] = f"rows: {r}"
        except Exception as e:
            msg = " ".join(str(e).split("DB::Exception:")[-1].split())
            row["clickhouse"] = "error: " + msg[:200]
        row["clickhouse_alive"] = ch.query("SELECT 1").result_rows == [(1,)]
        row["duckdb"] = run_duck(path)
        row["datafusion"] = run_df(path)
        row["seconds"] = round(time.perf_counter() - start, 2)
        results[name] = row
        print(f"[robust] {name}: {row}", flush=True)
    json.dump(results, open("/results/robustness.json", "w"), indent=1)
    lines = ["", "## Damaged segments (local file, `count(*)` for the project)", "",
             "| case | ClickHouse | server still up | DuckDB | DataFusion |", "|---|---|---|---|---|"]
    for name, r in results.items():
        cells = [name, r["clickhouse"], str(r["clickhouse_alive"]), r["duckdb"], r["datafusion"]]
        lines.append("| " + " | ".join(x.replace("|", "/") for x in cells) + " |")
    with open("/results/results.md", "a") as f:
        f.write("\n".join(lines) + "\n")


if __name__ == "__main__":
    main()
