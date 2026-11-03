"""Constants and helpers shared by the generator and the benchmark."""
import hashlib
import json
import time
from datetime import datetime, timezone

import paramiko

WORKSPACE = "0193a000-0000-7000-8000-000000000001"
PROJECT = "0193a000-0000-7000-8000-000000000002"
STREAM = "0193a000-0000-7000-8000-000000000003"
BUCKET = "archive"
DAY = "20261101"
DAY_START_MS = int(datetime(2026, 11, 1, tzinfo=timezone.utc).timestamp() * 1000)
DAY_END_MS = DAY_START_MS + 86_400_000
# Q1 and Q4 look at one hour in the afternoon.
WINDOW = (DAY_START_MS + 13 * 3_600_000, DAY_START_MS + 14 * 3_600_000)
WINDOW_SQL = ("2026-11-01 13:00:00", "2026-11-01 14:00:00")
NEEDLE = "needle-7f3a"
LIMIT = 100
ROW_GROUP = 100_000
LAYOUTS = {"day": 86_400_000, "hour": 3_600_000, "5min": 300_000}


def segment_key(layout: str, index: int) -> str:
    return f"{PROJECT}/{DAY}/{layout}/{index:04d}.parquet"


def digest(value) -> str:
    return hashlib.sha1(json.dumps(value).encode()).hexdigest()[:12]


def sftp_connect(attempts: int = 30) -> paramiko.SFTPClient:
    """Key auth with the host key pinned from the keys volume; an unknown host key is refused. Retries while the
    server starts; a host key mismatch is not retried."""
    for i in range(attempts):
        try:
            return _sftp_connect()
        except paramiko.BadHostKeyException:
            raise
        except Exception:
            if i == attempts - 1:
                raise
            time.sleep(2)


def _sftp_connect() -> paramiko.SFTPClient:
    host_key = paramiko.Ed25519Key.from_private_key_file("/keys/ssh_host_ed25519_key")
    client = paramiko.SSHClient()
    client.get_host_keys().add("sftp", "ssh-ed25519", host_key)
    client.set_missing_host_key_policy(paramiko.RejectPolicy())
    client.connect("sftp", username="archive", key_filename="/keys/client", look_for_keys=False, allow_agent=False,
                   timeout=30, disabled_algorithms={"pubkeys": []})
    return client.open_sftp()


def sftp_makedirs(sftp: paramiko.SFTPClient, path: str) -> None:
    cur = ""
    for part in path.split("/"):
        cur = f"{cur}/{part}" if cur else part
        try:
            sftp.stat(cur)
        except FileNotFoundError:
            sftp.mkdir(cur)
