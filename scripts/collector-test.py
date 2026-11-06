#!/usr/bin/env python3
"""Verify OTLP/HTTP JSON -> real Collector -> OTLP protobuf -> durable ingest -> search."""
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
IMAGE = "otel/opentelemetry-collector-contrib:0.139.0"


def main() -> None:
    project = f"ervlogs-collector-{os.getpid()}"
    app_port = int(os.environ.get("COLLECTOR_TEST_APP_PORT", "18092"))
    otlp_port = int(os.environ.get("COLLECTOR_TEST_OTLP_PORT", "14318"))
    base = f"http://127.0.0.1:{app_port}"
    setup_code = secrets.token_urlsafe(24)
    with tempfile.TemporaryDirectory(prefix="ervlogs-collector-") as directory:
        work = Path(directory)
        env_file = work / "stack.env"
        config_file = work / "collector.yaml"
        compose_file = work / "collector-compose.yaml"
        env_file.write_text("\n".join([
            f"POSTGRES_PASSWORD={secrets.token_urlsafe(24)}",
            f"CLICKHOUSE_PASSWORD={secrets.token_urlsafe(24)}",
            f"ERVISIO_LOGS_SECRET_KEY={secrets.token_urlsafe(48)}",
            f"ERVISIO_LOGS_PUBLIC_URL={base}",
            f"ERVISIO_LOGS_PORT={app_port}",
            f"ERVISIO_LOGS_SETUP_CODE={setup_code}",
        ]) + "\n")
        env_file.chmod(0o600)
        config_file.write_text("receivers: {}\n")
        config_file.chmod(0o644)
        compose_file.write_text(f"""services:
  collector:
    image: {IMAGE}
    command: ["--config=/etc/otelcol-contrib/config.yaml"]
    volumes: [{json.dumps(str(config_file) + ':/etc/otelcol-contrib/config.yaml:ro')}]
    ports: ["127.0.0.1:{otlp_port}:4318"]
    environment:
      ERVISIO_TEST_TOKEN: ${{ERVISIO_TEST_TOKEN:-}}
    depends_on:
      app: {{condition: service_healthy}}
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
networks:
  edge:
    name: {project}_edge
volumes:
  admin-socket:
    name: {project}_admin
""")
        command = ["docker", "compose", "-p", project, "--env-file", str(env_file),
                   "-f", str(ROOT / "deploy/compose.yaml"), "-f", str(compose_file)]
        environment = os.environ.copy()

        def compose(*args):
            subprocess.run(command + list(args), cwd=ROOT, env=environment, check=True)

        jar = http.cookiejar.CookieJar()
        browser = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
        csrf = ""

        def api(method, path, data=None, token=None):
            headers = {"Content-Type": "application/json"}
            if csrf:
                headers["X-CSRF-Token"] = csrf
            if token:
                headers["Authorization"] = "Bearer " + token
            request = urllib.request.Request(base + "/api/v1" + path, method=method,
                                             data=None if data is None else json.dumps(data).encode(), headers=headers)
            with browser.open(request, timeout=15) as response:
                return json.load(response)

        try:
            compose("up", "-d", "--build", "--wait", "--wait-timeout", "300", "app")
            account = api("POST", "/auth/register", {"email": "collector@example.com", "username": "collector",
                          "password": secrets.token_urlsafe(24), "setup_code": setup_code})
            csrf = account["csrf_token"]
            workspace = api("POST", "/workspaces", {"name": "Collector integration"})["id"]
            project_id = api("POST", "/projects", {"workspace_id": workspace, "name": "OTLP"})["id"]
            api("POST", f"/projects/{project_id}/streams", {"name": "collector"})
            token = api("POST", "/tokens", {"project_id": project_id, "name": "Collector fixture",
                         "scopes": ["logs:ingest", "logs:read"]})["token"]
            environment["ERVISIO_TEST_TOKEN"] = token
            config_file.write_text("""receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318
processors:
  batch:
    timeout: 1s
    send_batch_size: 10
exporters:
  otlphttp:
    endpoint: http://app:8080
    headers:
      Authorization: "Bearer ${env:ERVISIO_TEST_TOKEN}"
    compression: gzip
    encoding: proto
    retry_on_failure:
      enabled: true
      initial_interval: 1s
      max_interval: 5s
      max_elapsed_time: 20s
service:
  pipelines:
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlphttp]
""")
            config_file.chmod(0o644)
            if config_file.stat().st_mode & 0o777 != 0o644:
                raise RuntimeError("Collector config must be readable by its non-root container user")
            compose("run", "--rm", "--no-deps", "collector", "validate", "--config=/etc/otelcol-contrib/config.yaml")
            compose("up", "-d", "collector")
            payload = {"resourceLogs": [{"resource": {"attributes": [
                {"key": "service.name", "value": {"stringValue": "collector-fixture"}},
                {"key": "ervisio.stream", "value": {"stringValue": "collector"}}]},
                "scopeLogs": [{"scope": {"name": "ervisio.collector.test", "version": "1"}, "logRecords": [{
                    "timeUnixNano": str(time.time_ns()), "severityNumber": 17, "severityText": "ERROR",
                    "body": {"stringValue": "Collector protobuf and gzip integration"},
                    "attributes": [{"key": "fixture", "value": {"stringValue": project}}]}]}]}]}
            for attempt in range(30):
                try:
                    request = urllib.request.Request(f"http://127.0.0.1:{otlp_port}/v1/logs",
                              data=json.dumps(payload).encode(), headers={"Content-Type": "application/json"})
                    with urllib.request.urlopen(request, timeout=5) as response:
                        if response.status != 200:
                            raise RuntimeError("Collector did not accept the log")
                    break
                except (urllib.error.URLError, ConnectionError):
                    if attempt == 29:
                        raise
                    time.sleep(1)
            for attempt in range(40):
                found = api("POST", "/logs/search", {"project_id": project_id,
                            "filters": [{"field": "fixture", "op": "eq", "value": project}]}, token)
                if found["items"]:
                    item = found["items"][0]
                    if len(found["items"]) != 1 or item["stream"] != "collector" or item["service"] != "collector-fixture":
                        raise RuntimeError("Collector event was duplicated or mapped incorrectly")
                    print("COLLECTOR OK: real OTLP JSON receiver, gzip/protobuf exporter, durable ingest and scoped search")
                    return
                time.sleep(1)
            raise RuntimeError("Collector log did not reach searchable storage")
        finally:
            compose("down", "-v", "--remove-orphans")


if __name__ == "__main__":
    main()
