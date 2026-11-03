"""Counting S3 proxy. Every engine reaches S3 through it, so the benchmark sees the requests and bytes each engine
really transfers. It adds LATENCY_MS before each request to stand in for a remote provider (the S3 server runs on
the same Docker host). The Host header passes through unchanged, so SigV4 signatures stay valid.

Port 9000 serves S3; GET /stats and POST /reset on port 9001 read and clear the counters.
"""
import http.client
import json
import os
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

UPSTREAM = ("s3", 8333)
LATENCY = int(os.environ.get("LATENCY_MS", "25")) / 1000
_lock = threading.Lock()
_stats = {"requests": 0, "gets": 0, "heads": 0, "lists": 0, "bytes": 0}


def _count(**kw) -> None:
    with _lock:
        for k, v in kw.items():
            _stats[k] += v


class S3(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args) -> None:
        pass

    def _forward(self) -> None:
        time.sleep(LATENCY)
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length) if length else None
        conn = http.client.HTTPConnection(*UPSTREAM, timeout=300)
        headers = {k: v for k, v in self.headers.items() if k.lower() != "connection"}
        conn.request(self.command, self.path, body=body, headers=headers)
        resp = conn.getresponse()
        is_list = self.command == "GET" and ("list-type" in self.path or self.path.rstrip("/").count("/") <= 1)
        _count(requests=1, gets=int(self.command == "GET" and not is_list), heads=int(self.command == "HEAD"),
               lists=int(is_list))
        length = resp.getheader("Content-Length")
        # Without a length (chunked replies, small XML) buffer; otherwise stream, so a client that reads only part
        # of an open-ended range, then hangs up, costs only the bytes it received.
        data = resp.read() if length is None and self.command != "HEAD" else None
        self.send_response(resp.status)
        for k, v in resp.getheaders():
            if k.lower() not in ("transfer-encoding", "connection", "content-length"):
                self.send_header(k, v)
        self.send_header("Content-Length", str(len(data)) if data is not None else (length or "0"))
        self.end_headers()
        try:
            if data is not None:
                self.wfile.write(data)
                _count(bytes=len(data))
            elif self.command != "HEAD":
                while chunk := resp.read(256 * 1024):
                    self.wfile.write(chunk)
                    _count(bytes=len(chunk))
        except (BrokenPipeError, ConnectionResetError):
            self.close_connection = True  # the client stopped reading; that is normal for range readers
        finally:
            conn.close()

    do_GET = do_HEAD = do_PUT = do_POST = do_DELETE = _forward


class Control(BaseHTTPRequestHandler):
    def log_message(self, *args) -> None:
        pass

    def _reply(self, payload: dict) -> None:
        body = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self) -> None:
        with _lock:
            self._reply(dict(_stats))

    def do_POST(self) -> None:
        with _lock:
            for k in _stats:
                _stats[k] = 0
        self._reply({"ok": True})


def start() -> None:
    for port, handler in ((9000, S3), (9001, Control)):
        server = ThreadingHTTPServer(("0.0.0.0", port), handler)
        server.daemon_threads = True
        threading.Thread(target=server.serve_forever, daemon=True).start()


if __name__ == "__main__":
    start()
    threading.Event().wait()
