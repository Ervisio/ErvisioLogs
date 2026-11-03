"""D004 and D005 prototype on the product's ClickHouse image.

D004: projects have different local retention. With one table partitioned by day, expiring one project's day means
deleting rows inside a partition every tenant shares. The alternative adds the retention class to the partition key,
so expiry becomes DROP PARTITION. Measured: insert cost and part counts, query cost, and the cost of expiring the
oldest day of the 1-day-class projects with a lightweight DELETE, an ALTER DELETE mutation, or DROP PARTITION.

D005: compares what ClickHouse reports (system.parts bytes_on_disk) with what the disk holds (du on the data
directory), during a large merge and after DROP PARTITION, to find what the cache accountant has to count.

Writes /results/results.json and /results/results.md.
"""
import json
import os
import statistics
import subprocess
import threading
import time

import clickhouse_connect

PROJECTS = int(os.environ.get("PROJECTS", "24"))
DAYS = int(os.environ.get("DAYS", "4"))
ROWS = int(os.environ.get("ROWS_PER_PROJECT_DAY", "200000"))
CLASSES = [1, 3, 7, 14]  # local retention days; project i gets CLASSES[i % 4]
DAY0 = "2026-11-01"
SLICES_PER_DAY = 24  # one insert per hour of data, every project in each insert, like the ingest writer's batches


def log(msg: str) -> None:
    print(f"[d004 {time.strftime('%H:%M:%S')}] {msg}", flush=True)


ch = None


def q(sql: str, **params):
    return ch.query(sql, parameters=params).result_rows


def cmd(sql: str, settings=None) -> float:
    start = time.perf_counter()
    ch.command(sql, settings=settings or {})
    return round(time.perf_counter() - start, 3)


def connect():
    for _ in range(60):
        try:
            return clickhouse_connect.get_client(host="clickhouse", username="bench",
                                                 password=os.environ["CLICKHOUSE_PASSWORD"],
                                                 send_receive_timeout=3600)
        except Exception:
            time.sleep(2)
    raise SystemExit("clickhouse did not come up")


SCHEMA = """
(
    workspace_id    UUID,
    project_id      UUID,
    stream_id       UUID,
    local_days      UInt16,
    event_id        String,
    timestamp       DateTime64(3, 'UTC'),
    severity_number UInt8,
    severity_text   LowCardinality(String),
    message         String CODEC(ZSTD(3)),
    attributes      JSON
)
ENGINE = MergeTree
PARTITION BY {partition}
ORDER BY (workspace_id, project_id, stream_id, timestamp, event_id)
"""

TABLES = {
    "by_day": "toYYYYMMDD(timestamp)",
    "by_day_class": "(toYYYYMMDD(timestamp), local_days)",
}


def project_uuid(i: int) -> str:
    return f"0193a000-0000-7000-8000-{i:012d}"


def insert_sql(table: str, day: int, slice_: int) -> str:
    """Rows for every project inside one hour of one day, generated server-side."""
    per_project = ROWS // SLICES_PER_DAY
    n = per_project * PROJECTS
    start_ms = f"toUnixTimestamp64Milli(toDateTime64('{DAY0} 00:00:00', 3, 'UTC')) + {day} * 86400000 + {slice_} * 3600000"
    classes = "[" + ",".join(str(c) for c in CLASSES) + "]"
    return f"""
INSERT INTO {table}
SELECT
    toUUID('0193a000-0000-7000-8000-000000000000'),
    toUUID(concat('0193a000-0000-7000-8000-', leftPad(toString(p), 12, '0'))),
    toUUID('0193a000-0000-7000-8000-00000000ffff'),
    {classes}[p % {len(CLASSES)} + 1],
    concat('e', toString({day}), '-', toString({slice_}), '-', toString(number)),
    fromUnixTimestamp64Milli(toInt64({start_ms} + intDiv(number, {PROJECTS}) * 3600000 / {per_project}), 'UTC'),
    [1, 5, 9, 13, 17][1 + (cityHash64(number) % 5)],
    ['trace', 'debug', 'info', 'warn', 'error'][1 + (cityHash64(number) % 5)],
    concat(['player joined', 'payment failed', 'vehicle spawned', 'db query slow'][1 + (cityHash64(number, 1) % 4)],
           ' id=', toString(cityHash64(number, 2) % 1000000)),
    concat('{{"user":"u', toString(cityHash64(number, 3) % 50000), '","map":"',
           ['harbor', 'airport', 'downtown'][1 + (cityHash64(number, 4) % 3)], '","ping":',
           toString(cityHash64(number, 5) % 400), '}}')
FROM (SELECT number, number % {PROJECTS} AS p FROM numbers({n}))
"""


def data_dir(table: str) -> str:
    path = q("SELECT data_paths[1] FROM system.tables WHERE database = currentDatabase() AND name = {t:String}", t=table)[0][0]
    return path.replace("/var/lib/clickhouse/", "/ch/", 1)


def du(path: str) -> int:
    out = subprocess.run(["du", "-sb", path], capture_output=True, text=True)
    return int(out.stdout.split()[0]) if out.returncode == 0 else -1


def parts(table: str) -> dict:
    rows = q("""SELECT active, count(), sum(bytes_on_disk), uniqExact(partition) FROM system.parts
                WHERE database = currentDatabase() AND table = {t:String} GROUP BY active""", t=table)
    out = {"active_parts": 0, "active_bytes": 0, "inactive_parts": 0, "inactive_bytes": 0, "partitions": 0}
    for active, n, b, p in rows:
        key = "active" if active else "inactive"
        out[f"{key}_parts"], out[f"{key}_bytes"] = n, b
        if active:
            out["partitions"] = p
    return out


def load() -> dict:
    out = {}
    for table, partition in TABLES.items():
        cmd(f"DROP TABLE IF EXISTS {table} SYNC")
        cmd(f"CREATE TABLE {table} " + SCHEMA.format(partition=partition))
        # Background merges off during the load, so both tables get the same number of inserts and new parts.
        cmd(f"SYSTEM STOP MERGES {table}")
        start = time.perf_counter()
        for day in range(DAYS):
            for s in range(SLICES_PER_DAY):
                ch.command(insert_sql(table, day, s))
        wall = round(time.perf_counter() - start, 1)
        cmd("SYSTEM FLUSH LOGS")
        new_parts = q("SELECT count() FROM system.part_log WHERE table = {t:String} AND event_type = 'NewPart'", t=table)[0][0]
        before = parts(table)
        cmd(f"SYSTEM START MERGES {table}")
        # Let background merges settle the way they would in production.
        for _ in range(120):
            if not q("SELECT count() FROM system.merges WHERE table = {t:String}", t=table)[0][0] and \
                    parts(table)["active_parts"] <= before["partitions"] * 3:
                break
            time.sleep(5)
        out[table] = {"insert_s": wall, "inserts": DAYS * SLICES_PER_DAY, "new_parts": new_parts,
                      "partitions": before["partitions"], "parts_after_merges": parts(table)["active_parts"],
                      "rows": q(f"SELECT count() FROM {table}")[0][0]}
        log(f"load {table}: {out[table]}")
    return out


def query_cost() -> dict:
    project = project_uuid(5)
    queries = {
        "hour_page": "SELECT event_id FROM {t} WHERE project_id = toUUID('{p}') AND timestamp >= toDateTime64('2026-11-02 13:00:00', 3, 'UTC') "
                     "AND timestamp < toDateTime64('2026-11-02 14:00:00', 3, 'UTC') ORDER BY timestamp DESC, event_id DESC LIMIT 100",
        "day_levels": "SELECT severity_text, count() FROM {t} WHERE project_id = toUUID('{p}') AND toDate(timestamp) = '2026-11-02' GROUP BY severity_text",
        "all_days_attr": "SELECT count() FROM {t} WHERE project_id = toUUID('{p}') AND attributes.user::String = 'u42'",
    }
    out = {}
    for table in TABLES:
        for name, sql in queries.items():
            runs = []
            for i in range(3):
                tag = f"d004-{table}-{name}-{i}"
                ch.query(sql.format(t=table, p=project), settings={"log_comment": tag, "use_query_condition_cache": 0})
                runs.append(tag)
            cmd("SYSTEM FLUSH LOGS")
            rows = q("""SELECT query_duration_ms, read_rows, read_bytes, ProfileEvents['SelectedParts'], ProfileEvents['SelectedMarks']
                        FROM system.query_log WHERE type = 'QueryFinish' AND log_comment IN {tags:Array(String)}""", tags=runs)
            out[f"{table}/{name}"] = {
                "ms": statistics.median(r[0] for r in rows), "read_rows": rows[0][1], "read_mb": round(rows[0][2] / 1e6, 1),
                "parts": rows[0][3], "marks": rows[0][4]}
            log(f"query {table}/{name}: {out[f'{table}/{name}']}")
    return out


def expire() -> dict:
    """Expire 2026-11-01 for the projects of the 1-day class, three ways."""
    one_day = [project_uuid(i) for i in range(PROJECTS) if CLASSES[i % len(CLASSES)] == 1]
    in_list = ",".join(f"toUUID('{p}')" for p in one_day)
    where = f"project_id IN ({in_list}) AND toYYYYMMDD(timestamp) = 20261101"
    expected = q(f"SELECT count() FROM by_day WHERE {where}")[0][0]
    out = {"rows_to_expire": expected, "projects_expired": len(one_day)}

    # Same starting parts for both delete methods: by_day_mut gets hard links of by_day's parts.
    cmd("DROP TABLE IF EXISTS by_day_mut SYNC")
    cmd("CREATE TABLE by_day_mut AS by_day")
    for (part,) in q("SELECT DISTINCT partition_id FROM system.parts WHERE table = 'by_day' AND active"):
        cmd(f"ALTER TABLE by_day_mut ATTACH PARTITION ID '{part}' FROM by_day")

    def measure(table: str, action, reclaim=None) -> dict:
        cmd("SYSTEM FLUSH LOGS")
        since = q("SELECT now64(3)")[0][0]
        disk0, parts0 = du(data_dir(table)), parts(table)
        t = action()
        disk1 = du(data_dir(table))
        r = {"seconds": t, "du_before_mb": round(disk0 / 1e6, 1), "du_after_mb": round(disk1 / 1e6, 1),
             "active_mb_before": round(parts0["active_bytes"] / 1e6, 1)}
        if reclaim:
            r["reclaim_seconds"] = reclaim()
        # Old parts stay on disk until the cleanup thread removes them; wait and measure again.
        for _ in range(60):
            if parts(table)["inactive_parts"] == 0:
                break
            time.sleep(5)
        cmd("SYSTEM FLUSH LOGS")
        written = q("""SELECT event_type, count(), sum(size_in_bytes) FROM system.part_log
                       WHERE table = {t:String} AND event_time_microseconds >= {s:DateTime64(3)}
                       AND event_type IN ('MergeParts', 'MutatePart') GROUP BY event_type""", t=table, s=since)
        r.update({"du_settled_mb": round(du(data_dir(table)) / 1e6, 1),
                  "active_mb_after": round(parts(table)["active_bytes"] / 1e6, 1),
                  "rewritten": {e: {"parts": n, "mb": round(b / 1e6, 1)} for e, n, b in written},
                  "left_rows": q(f"SELECT count() FROM {table} WHERE {where}")[0][0]})
        return r

    out["lightweight_delete"] = measure(
        "by_day", lambda: cmd(f"DELETE FROM by_day WHERE {where}", {"lightweight_deletes_sync": 2}),
        reclaim=lambda: cmd("OPTIMIZE TABLE by_day PARTITION 20261101 FINAL"))
    log(f"expire lightweight: {out['lightweight_delete']}")
    out["alter_delete"] = measure("by_day_mut", lambda: cmd(f"ALTER TABLE by_day_mut DELETE WHERE {where}",
                                                             {"mutations_sync": 2}))
    log(f"expire mutation: {out['alter_delete']}")
    out["drop_partition"] = measure("by_day_class", lambda: cmd("ALTER TABLE by_day_class DROP PARTITION (20261101, 1)"))
    log(f"expire drop: {out['drop_partition']}")
    return out


def merge_accounting() -> dict:
    """D005: disk use against system.parts while one large merge runs."""
    table = "by_day_class"
    path = data_dir(table)
    samples = []
    done = threading.Event()

    def sample():
        while not done.is_set():
            p = parts(table)
            samples.append({"t": time.perf_counter(), "du": du(path), **p,
                            "merging_mb": q("SELECT sum(total_size_bytes_compressed) FROM system.merges WHERE table = {t:String}", t=table)[0][0] or 0})
            time.sleep(0.5)

    steady = parts(table)
    du0 = du(path)
    th = threading.Thread(target=sample)
    th.start()
    start = time.perf_counter()
    cmd(f"OPTIMIZE TABLE {table} FINAL")
    merge_s = round(time.perf_counter() - start, 1)
    time.sleep(3)
    done.set()
    th.join()
    peak = max(samples, key=lambda s: s["du"]) if samples else {"du": du0}
    after = parts(table)
    return {
        "before": {"du_mb": round(du0 / 1e6, 1), "active_mb": round(steady["active_bytes"] / 1e6, 1),
                   "inactive_mb": round(steady["inactive_bytes"] / 1e6, 1)},
        "merge_seconds": merge_s,
        "peak_du_mb": round(peak["du"] / 1e6, 1),
        "peak_ratio_to_active_before": round(peak["du"] / max(steady["active_bytes"], 1), 2),
        "after_merge": {"du_mb": round(du(path) / 1e6, 1), "active_mb": round(after["active_bytes"] / 1e6, 1),
                        "inactive_mb": round(after["inactive_bytes"] / 1e6, 1), "inactive_parts": after["inactive_parts"]},
        "old_parts_lifetime_s": int(q("SELECT value FROM system.merge_tree_settings WHERE name = 'old_parts_lifetime'")[0][0]),
        "samples": len(samples),
    }


def main() -> None:
    global ch
    ch = connect()
    version = q("SELECT version()")[0][0]
    log(f"clickhouse {version}: {PROJECTS} projects x {DAYS} days x {ROWS:,} rows, classes {CLASSES}")
    result = {"meta": {"clickhouse": version, "projects": PROJECTS, "days": DAYS, "rows_per_project_day": ROWS,
                       "classes": CLASSES, "inserts_per_day": SLICES_PER_DAY}}
    result["load"] = load()
    result["queries"] = query_cost()
    result["expire"] = expire()
    result["merge_accounting"] = merge_accounting()
    log(f"merge accounting: {result['merge_accounting']}")
    json.dump(result, open("/results/results.json", "w"), indent=1, default=str)
    write_markdown(result)
    log("done")


def write_markdown(r: dict) -> None:
    m = r["meta"]
    lines = ["# D004 and D005 results", "",
             f"ClickHouse {m['clickhouse']}. {m['projects']} projects, {m['days']} days, {m['rows_per_project_day']:,} rows per "
             f"project-day, local retention classes {m['classes']} assigned round robin. {m['inserts_per_day']} inserts per day, "
             "each holding every project.", "", "## Load", "",
             "| table | partitions | inserts | new parts | parts after merges | rows | insert s |", "|---|---|---|---|---|---|---|"]
    for t, v in r["load"].items():
        lines.append(f"| {t} | {v['partitions']} | {v['inserts']} | {v['new_parts']} | {v['parts_after_merges']} | {v['rows']:,} | {v['insert_s']} |")
    lines += ["", "## Queries (one project, median of 3)", "", "| table/query | ms | rows read | MB read | parts | marks |",
              "|---|---|---|---|---|---|"]
    for k, v in r["queries"].items():
        lines.append(f"| {k} | {v['ms']} | {v['read_rows']:,} | {v['read_mb']} | {v['parts']} | {v['marks']} |")
    e = r["expire"]
    lines += ["", f"## Expiring one day of the 1-day class ({e['projects_expired']} projects, {e['rows_to_expire']:,} rows)", "",
              "| method | seconds | reclaim s | du before MB | du right after MB | du settled MB | rewritten | rows left |",
              "|---|---|---|---|---|---|---|---|"]
    for k in ("lightweight_delete", "alter_delete", "drop_partition"):
        v = e[k]
        rw = ", ".join(f"{ev} {x['parts']} parts {x['mb']} MB" for ev, x in v["rewritten"].items()) or "none"
        lines.append(f"| {k} | {v['seconds']} | {v.get('reclaim_seconds', '')} | {v['du_before_mb']} | {v['du_after_mb']} | "
                     f"{v['du_settled_mb']} | {rw} | {v['left_rows']} |")
    a = r["merge_accounting"]
    lines += ["", "## D005: disk against system.parts during OPTIMIZE FINAL of by_day_class", "",
              f"- before: du {a['before']['du_mb']} MB, active parts {a['before']['active_mb']} MB, inactive {a['before']['inactive_mb']} MB",
              f"- merge took {a['merge_seconds']} s; peak du {a['peak_du_mb']} MB, {a['peak_ratio_to_active_before']}x the active bytes before",
              f"- right after: du {a['after_merge']['du_mb']} MB, active {a['after_merge']['active_mb']} MB, "
              f"inactive {a['after_merge']['inactive_mb']} MB in {a['after_merge']['inactive_parts']} parts "
              f"(removed after old_parts_lifetime = {a['old_parts_lifetime_s']} s)", ""]
    open("/results/results.md", "w").write("\n".join(lines))


if __name__ == "__main__":
    main()
