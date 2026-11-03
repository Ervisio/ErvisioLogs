"""D001 benchmark. For every layout and query it plans the segments from the manifest (the catalog's job), then runs
each engine against them:

  s3    the engine reads the objects itself, through the counting proxy (requests, bytes, latency)
  sftp  no engine speaks SFTP, so the planned files are downloaded to the local cache first and then queried;
        the download is timed on its own and the query is timed against the local files

Every run starts cold: DuckDB and DataFusion run in a fresh process, ClickHouse runs with its caches disabled.
Writes /results/results.json and /results/results.md.
"""
import json
import os
import shutil
import socket
import statistics
import subprocess
import tempfile
import time
import urllib.request

import clickhouse_connect

import common as c
import proxy
import queries as q

# `compose run` containers get no service alias, so ClickHouse reaches the proxy by this container's address.
PROXY = f"{socket.gethostbyname(socket.gethostname())}:9000"
RUNS = int(os.environ.get("RUNS", "3"))
ENGINES = ["clickhouse", "duckdb", "datafusion"]


def log(msg: str) -> None:
    print(f"[bench {time.strftime('%H:%M:%S')}] {msg}", flush=True)


def proxy_call(method: str, path: str) -> dict:
    req = urllib.request.Request(f"http://localhost:9001{path}", method=method)
    with urllib.request.urlopen(req) as r:
        return json.load(r)


def plan(manifest: dict, layout: str, query: str) -> list[dict]:
    lo, hi = q.window_ms(query)
    return [s for s in manifest["segments"] if s["layout"] == layout and s["min_ms"] < hi and s["max_ms"] > lo]


def brace(segs: list[dict]) -> str:
    """ClickHouse glob for the planned files: they share a directory and differ only in their index."""
    prefix = os.path.dirname(segs[0]["key"])
    names = ",".join(f"{s['index']:04d}" for s in segs)
    return f"{prefix}/{{{names}}}.parquet" if len(segs) > 1 else segs[0]["key"]


class ClickHouse:
    def __init__(self) -> None:
        for _ in range(60):
            try:
                self.client = clickhouse_connect.get_client(host="clickhouse", username="bench",
                                                            password=os.environ["CLICKHOUSE_PASSWORD"])
                break
            except Exception:
                time.sleep(2)
        else:
            raise SystemExit("clickhouse did not come up")
        self.version = self.client.query("SELECT version()").result_rows[0][0]
        # Cold runs: turn off every cache that could answer a repeated query from memory.
        wanted = ["use_query_cache", "use_query_condition_cache", "schema_inference_use_cache_for_s3",
                  "schema_inference_use_cache_for_file", "input_format_parquet_use_metadata_cache",
                  "use_cache_for_count_from_files", "enable_filesystem_cache", "use_page_cache_for_disks_without_file_cache"]
        present = {r[0] for r in self.client.query(
            "SELECT name FROM system.settings WHERE name IN %(n)s", parameters={"n": wanted}).result_rows}
        self.settings = {name: 0 for name in wanted if name in present}
        log(f"clickhouse {self.version}, caches off: {sorted(self.settings)}")
        self.n = 0

    def run(self, source: str, query: str, segs: list[dict]) -> dict:
        if source == "s3":
            src = (f"s3('http://{PROXY}/{c.BUCKET}/{brace(segs)}', '{os.environ['S3_ACCESS_KEY']}', "
                   f"'{os.environ['S3_SECRET_KEY']}', 'Parquet')")
        else:
            src = f"file('cache/{brace(segs)}', 'Parquet')"
        self.n += 1
        tag = f"d001-{os.getpid()}-{self.n}"
        settings = dict(self.settings, log_comment=tag)
        start = time.perf_counter()
        try:
            rows = self.client.query(q.sql("clickhouse", query, src), settings=settings).result_rows
            error = None
        except Exception as e:
            rows, error = [], f"{type(e).__name__}: {e}"[:300]
        wall = time.perf_counter() - start
        self.client.command("SYSTEM FLUSH LOGS")
        mem = self.client.query(
            "SELECT max(memory_usage) FROM system.query_log WHERE log_comment = %(t)s AND type = 'QueryFinish'",
            parameters={"t": tag}).result_rows[0][0]
        return {"rows": len(rows), "digest": q.result_digest(query, rows) if rows else None, "wall_s": round(wall, 3),
                "peak_mem_mb": round((mem or 0) / 2**20, 1), "error": error}


def subprocess_run(engine: str, source: str, query: str, segs: list[dict], baseline_mb: float) -> dict:
    with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as f:
        json.dump([s["key"] for s in segs], f)
    try:
        out = subprocess.run(["python", "run_one.py", engine, source, query, f.name], capture_output=True, text=True,
                             timeout=1800)
    finally:
        os.unlink(f.name)
    if out.returncode != 0:
        return {"rows": 0, "digest": None, "wall_s": None, "peak_mem_mb": None, "error": out.stderr[-300:]}
    r = json.loads(out.stdout.strip().splitlines()[-1])
    r["peak_mem_mb"] = round(r.pop("peak_rss_mb") - baseline_mb, 1)
    return r


def download(sftp, segs: list[dict]) -> dict:
    """Fetches the planned files into the cache; the cache starts empty each time so every download is cold."""
    for entry in os.listdir("/cache"):
        shutil.rmtree(os.path.join("/cache", entry), ignore_errors=True)
    start = time.perf_counter()
    total = 0
    for s in segs:
        local = f"/cache/{s['key']}"
        os.makedirs(os.path.dirname(local), exist_ok=True)
        sftp.get(f"upload/{s['key']}", local + ".part")
        size = os.path.getsize(local + ".part")
        if size != s["bytes"]:
            raise SystemExit(f"sftp download size mismatch for {s['key']}")
        os.rename(local + ".part", local)
        total += size
    return {"wall_s": round(time.perf_counter() - start, 3), "bytes": total}


def median(values):
    values = [v for v in values if v is not None]
    return round(statistics.median(values), 3) if values else None


def summarize(runs: list[dict], expected: str) -> dict:
    return {
        "wall_s": median([r["wall_s"] for r in runs]),
        "wall_s_runs": [r["wall_s"] for r in runs],
        "peak_mem_mb": median([r.get("peak_mem_mb") for r in runs]),
        "requests": median([r.get("requests") for r in runs]),
        "mb_read": median([r["bytes"] / 1e6 if r.get("bytes") is not None else None for r in runs]),
        "correct": all(r["digest"] == expected for r in runs),
        "errors": sorted({r["error"] for r in runs if r["error"]}),
    }


def main() -> None:
    manifest = json.load(open("/data/manifest.json"))
    proxy.start()
    ch = ClickHouse()
    baseline_mb = {}
    for engine in ("duckdb", "datafusion"):
        out = subprocess.run(["python", "run_one.py", engine, "baseline", "q4_page", "/dev/null"], capture_output=True,
                             text=True)
        baseline_mb[engine] = json.loads(out.stdout.strip().splitlines()[-1])["peak_rss_mb"]
    log(f"python baseline RSS with only the engine imported: {baseline_mb}")

    sftp = c.sftp_connect()

    results = []
    for layout in c.LAYOUTS:
        for query in q.QUERIES:
            segs = plan(manifest, layout, query)
            expected = manifest["truth"][query]
            seg_mb = round(sum(s["bytes"] for s in segs) / 1e6, 1)
            log(f"{layout} {query}: {len(segs)} files, {seg_mb} MB planned")

            for engine in ENGINES:
                runs = []
                for _ in range(RUNS):
                    proxy_call("POST", "/reset")
                    r = ch.run("s3", query, segs) if engine == "clickhouse" else \
                        subprocess_run(engine, "s3", query, segs, baseline_mb[engine])
                    r.update({k: v for k, v in proxy_call("GET", "/stats").items() if k in ("requests", "bytes")})
                    runs.append(r)
                results.append({"layout": layout, "query": query, "files": len(segs), "planned_mb": seg_mb,
                                "engine": engine, "source": "s3", **summarize(runs, expected)})
                log(f"  s3   {engine:10s} {results[-1]['wall_s']}s {results[-1]['requests']} req "
                    f"{results[-1]['mb_read']} MB correct={results[-1]['correct']} {results[-1]['errors'] or ''}")

            downloads = [download(sftp, segs) for _ in range(RUNS)]
            dl = {"wall_s": median([d["wall_s"] for d in downloads]), "mb": round(downloads[0]["bytes"] / 1e6, 1)}
            for engine in ENGINES:
                runs = []
                for _ in range(RUNS):
                    r = ch.run("local", query, segs) if engine == "clickhouse" else \
                        subprocess_run(engine, "local", query, segs, baseline_mb[engine])
                    runs.append(r)
                s = summarize(runs, expected)
                s.update({"download_s": dl["wall_s"], "mb_read": dl["mb"], "requests": None,
                          "total_s": round(dl["wall_s"] + s["wall_s"], 3) if s["wall_s"] is not None else None})
                results.append({"layout": layout, "query": query, "files": len(segs), "planned_mb": seg_mb,
                                "engine": engine, "source": "sftp", **s})
                log(f"  sftp {engine:10s} download {dl['wall_s']}s + query {s['wall_s']}s correct={s['correct']} "
                    f"{s['errors'] or ''}")

    meta = {"events": manifest["events"], "runs": RUNS, "latency_ms": proxy.LATENCY * 1000,
            "clickhouse": ch.version, "clickhouse_settings": ch.settings, "python_baseline_mb": baseline_mb,
            "segments": {l: {"files": sum(1 for s in manifest["segments"] if s["layout"] == l),
                             "mb": round(sum(s["bytes"] for s in manifest["segments"] if s["layout"] == l) / 1e6, 1)}
                         for l in c.LAYOUTS}}
    json.dump({"meta": meta, "results": results}, open("/results/results.json", "w"), indent=1)
    write_markdown(meta, results)
    log("done")


def write_markdown(meta: dict, results: list[dict]) -> None:
    lines = [f"# D001 results", "",
             f"{meta['events']:,} events in one project-day, median of {meta['runs']} cold runs, "
             f"{meta['latency_ms']:.0f} ms added to each S3 request. ClickHouse {meta['clickhouse']}.", "",
             "| layout | files in layout | size |", "|---|---|---|"]
    lines += [f"| {l} | {v['files']} | {v['mb']} MB |" for l, v in meta["segments"].items()]
    lines += ["", "## S3 (engine reads the objects)", "",
              "| layout | query | files | engine | wall s | requests | MB read | peak MB | correct |",
              "|---|---|---|---|---|---|---|---|---|"]
    for r in results:
        if r["source"] == "s3":
            lines.append(f"| {r['layout']} | {r['query']} | {r['files']} | {r['engine']} | {r['wall_s']} | "
                         f"{r['requests']} | {r['mb_read']} | {r['peak_mem_mb']} | "
                         f"{'yes' if r['correct'] else 'NO ' + '; '.join(r['errors'])} |")
    lines += ["", "## SFTP (download to cache, then query locally)", "",
              "| layout | query | files | MB downloaded | download s | engine | query s | total s | peak MB | correct |",
              "|---|---|---|---|---|---|---|---|---|---|"]
    for r in results:
        if r["source"] == "sftp":
            lines.append(f"| {r['layout']} | {r['query']} | {r['files']} | {r['mb_read']} | {r['download_s']} | "
                         f"{r['engine']} | {r['wall_s']} | {r['total_s']} | {r['peak_mem_mb']} | "
                         f"{'yes' if r['correct'] else 'NO ' + '; '.join(r['errors'])} |")
    lines += ["", "Peak MB: ClickHouse `system.query_log.memory_usage`; DuckDB and DataFusion process peak RSS minus "
              "the RSS of a process that only imports that engine.", ""]
    open("/results/results.md", "w").write("\n".join(lines))


if __name__ == "__main__":
    main()
