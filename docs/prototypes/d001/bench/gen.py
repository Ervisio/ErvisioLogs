"""Generates one project-day of synthetic events, writes it as Parquet in three segment layouts, uploads every
segment to S3 and to SFTP, and records the ground truth for each benchmark query.

Layouts (same rows, same sort order):
  day   one segment for the day            (larger than the 32-256 MB target)
  hour  24 segments                        (inside the target for a busy project)
  5min  288 segments                       (the tiny-file case the spec warns about)
"""
import hashlib
import json
import os
import time
from datetime import datetime, timezone

import boto3
import numpy as np
import paramiko
import pyarrow as pa
import pyarrow.compute as pc
import pyarrow.parquet as pq

import common as c

N = int(os.environ.get("EVENTS", "10000000"))
rng = np.random.default_rng(20261101)


def log(msg: str) -> None:
    print(f"[gen {time.strftime('%H:%M:%S')}] {msg}", flush=True)


def choice(values: list[str], n: int, p=None) -> pa.Array:
    idx = rng.choice(len(values), size=n, p=p)
    return pa.array(np.asarray(values, dtype=object)[idx], pa.string())


def build() -> pa.Table:
    log(f"building {N:,} events")
    ts = np.sort(rng.integers(c.DAY_START_MS, c.DAY_END_MS, size=N, dtype=np.int64))
    levels = ["debug", "info", "warn", "error", "fatal"]
    sev = rng.choice(5, size=N, p=[0.30, 0.50, 0.12, 0.07, 0.01])
    verbs = ["player joined", "player left", "vehicle spawned", "payment failed", "inventory saved", "zone entered",
             "job started", "job finished", "resource restarted", "chat message", "anticheat flag", "db query slow"]
    numbers = pa.array(rng.integers(0, 1_000_000, size=N).astype(str))
    took = pa.array(rng.integers(1, 5000, size=N).astype(str))
    message = pc.binary_join_element_wise(choice(verbs, N), pa.scalar("id="), numbers, pa.scalar("took"), took, " ")
    message = pc.binary_join_element_wise(message, pa.scalar("ms"), "")

    # The needle: 37 hits inside the Q1 window, 13 outside it, so the time filter has to work.
    t0, t1 = c.WINDOW
    inside = np.flatnonzero((ts >= t0) & (ts < t1))
    outside = np.flatnonzero((ts < t0) | (ts >= t1))
    hits = np.concatenate([rng.choice(inside, 37, replace=False), rng.choice(outside, 13, replace=False)])
    msg = message.to_numpy(zero_copy_only=False)
    for i in hits:
        msg[i] = f"card declined {c.NEEDLE.upper()} order={i}"  # upper case: the search is case-insensitive

    users = np.char.add("u", rng.integers(0, 50_000, size=N).astype(str)).astype(object)
    users[rng.random(N) < 0.0005] = "ada"
    maps = ["harbor", "airport", "downtown", "desert", "mountain"]
    attributes = pc.binary_join_element_wise(
        pa.scalar('{"user":"'), pa.array(users, pa.string()), pa.scalar('","map":"'), choice(maps, N),
        pa.scalar('","ping":'), pa.array(rng.integers(5, 400, size=N).astype(str)), pa.scalar("}"), "")

    return pa.table({
        "workspace_id": pa.array([c.WORKSPACE] * N, pa.string()).dictionary_encode(),
        "project_id": pa.array([c.PROJECT] * N, pa.string()).dictionary_encode(),
        "stream_id": pa.array([c.STREAM] * N, pa.string()).dictionary_encode(),
        "event_id": pa.array(np.char.add("e", np.char.zfill(np.arange(N).astype(str), 10)).astype(object), pa.string()),
        "timestamp": pa.array(ts, pa.timestamp("ms", tz="UTC")),
        "severity_number": pa.array((sev * 4 + 5).astype(np.uint8)),
        "severity_text": pa.array(np.asarray(levels, dtype=object)[sev], pa.string()).dictionary_encode(),
        "event_name": choice([v.replace(" ", ".") for v in verbs], N).dictionary_encode(),
        "message": pa.array(msg, pa.string()),
        "source_service": choice(["gameserver", "api", "worker"], N).dictionary_encode(),
        "source_host": choice([f"node-{i}" for i in range(8)], N).dictionary_encode(),
        "attributes": attributes,
    })


def truth(table: pa.Table) -> dict:
    t0, t1 = (pa.scalar(v, pa.timestamp("ms", tz="UTC")) for v in c.WINDOW)
    ts = table["timestamp"]
    in_window = pc.and_(pc.greater_equal(ts, t0), pc.less(ts, t1))

    def top(mask) -> list[str]:
        sub = table.filter(mask).select(["timestamp", "event_id"])
        sub = sub.sort_by([("timestamp", "descending"), ("event_id", "descending")])
        return sub["event_id"].to_pylist()[: c.LIMIT]

    needle = pc.match_substring(pc.utf8_lower(table["message"]), c.NEEDLE)
    ada = pc.match_substring(table["attributes"], '"user":"ada"')
    levels = table.group_by("severity_text").aggregate([("severity_text", "count")])
    return {
        "q1_needle": c.digest(top(pc.and_(in_window, needle))),
        "q2_attr": c.digest(top(ada)),
        "q3_levels": c.digest(sorted((str(k), int(v)) for k, v in zip(levels["severity_text"].to_pylist(), levels["severity_text_count"].to_pylist()))),
        "q4_page": c.digest(top(in_window)),
    }


def segments(table: pa.Table):
    ts = table["timestamp"].cast(pa.int64()).to_numpy()
    for layout, step in c.LAYOUTS.items():
        bounds = list(range(c.DAY_START_MS, c.DAY_END_MS, step)) + [c.DAY_END_MS]
        for i in range(len(bounds) - 1):
            lo, hi = np.searchsorted(ts, [bounds[i], bounds[i + 1]])
            yield layout, i, bounds[i], bounds[i + 1], table.slice(lo, hi - lo)


def main() -> None:
    os.makedirs("/data/segments", exist_ok=True)
    table = build()
    log("computing ground truth")
    expected = truth(table)

    s3 = boto3.client("s3", endpoint_url="http://s3:8333", aws_access_key_id=os.environ["S3_ACCESS_KEY"],
                      aws_secret_access_key=os.environ["S3_SECRET_KEY"], region_name="us-east-1")
    for _ in range(60):
        try:
            s3.create_bucket(Bucket=c.BUCKET)
            break
        except s3.exceptions.BucketAlreadyOwnedByYou:
            break
        except Exception:  # SeaweedFS takes a few seconds to elect a master
            time.sleep(2)
    sftp = c.sftp_connect()

    manifest = []
    for layout, i, lo, hi, part in segments(table):
        key = c.segment_key(layout, i)
        path = f"/data/segments/{key}"
        os.makedirs(os.path.dirname(path), exist_ok=True)
        pq.write_table(part, path, row_group_size=c.ROW_GROUP, compression="zstd", compression_level=3)
        size = os.path.getsize(path)
        sha = hashlib.sha256(open(path, "rb").read()).hexdigest()

        s3.upload_file(path, c.BUCKET, key)
        if s3.head_object(Bucket=c.BUCKET, Key=key)["ContentLength"] != size:
            raise SystemExit(f"S3 size mismatch for {key}")

        # SFTP: upload under a temporary name, check the size, then rename inside the same directory.
        remote = f"upload/{key}"
        c.sftp_makedirs(sftp, os.path.dirname(remote))
        sftp.put(path, remote + ".tmp")
        if sftp.stat(remote + ".tmp").st_size != size:
            raise SystemExit(f"SFTP size mismatch for {key}")
        sftp.posix_rename(remote + ".tmp", remote)

        manifest.append({"layout": layout, "index": i, "key": key, "min_ms": lo, "max_ms": hi,
                         "rows": part.num_rows, "bytes": size, "sha256": sha})
        if layout != "5min" or i % 48 == 0:
            log(f"{key}: {part.num_rows:,} rows, {size / 1e6:.1f} MB")

    json.dump({"events": N, "generated": datetime.now(timezone.utc).isoformat(), "truth": expected,
               "segments": manifest}, open("/data/manifest.json", "w"), indent=1)
    log("done")


if __name__ == "__main__":
    main()
