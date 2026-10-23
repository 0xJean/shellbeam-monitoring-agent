#!/usr/bin/env python3
"""Run a real Monitoring Agent against a private, loopback ShellBeam Server.

Only Python's standard library is required. Both binaries must already exist.
The test creates its own databases, administrator, node and private Agent config;
it never reads an App profile, starts a service or uses an existing server. Agent
self-update, remote control, notifications and public-IP lookups are disabled.
Run as an unprivileged user: on Linux the Agent's warning cleanup can inspect
/etc/motd, so this test deliberately refuses to run as root on Linux.

Output contains check names and cleanup status only, never credentials, API
bodies, hardware information or process logs.
"""

import argparse
from datetime import datetime, timezone
import hashlib
import http.client
from http.cookies import SimpleCookie
import json
import math
import os
from pathlib import Path
import secrets
import signal
import socket
import subprocess
import sys
import tempfile
import time
from urllib.parse import quote


MAX_RESPONSE = 2 * 1024 * 1024
REQUEST_TIMEOUT = 5


def require(condition):
    if not condition:
        raise AssertionError("contract check failed")


def timestamp(value):
    require(isinstance(value, str))
    result = datetime.fromisoformat(value.replace("Z", "+00:00"))
    require(result.tzinfo is not None)
    return result.timestamp()


def timestamp_millis(value):
    # The metric store preserves source receive times at millisecond precision;
    # recent reports retain Go's nanoseconds. Compare the same stored precision
    # without rounding a floating-point Unix timestamp across a millisecond.
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    require(parsed.tzinfo is not None)
    elapsed = parsed - datetime(1970, 1, 1, tzinfo=timezone.utc)
    return (elapsed.days * 86400 + elapsed.seconds) * 1000 + elapsed.microseconds // 1000


def sha256(path):
    digest = hashlib.sha256()
    with open(path, "rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def stop_process(process):
    if process is None:
        return True
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)
    process.wait(timeout=1)
    return process.poll() is not None


class Smoke:
    def __init__(self, server_binary, agent_binary, expected_version, root, logs):
        self.server_binary = server_binary
        self.agent_binary = agent_binary
        self.expected_version = expected_version
        self.root, self.logs = root, logs
        self.server, self.agent = None, None
        self.stage, self.checks = "allocate-loopback-port", []
        self.auth = None
        self.node_id = None
        self.agent_hash = sha256(agent_binary)
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            self.port = listener.getsockname()[1]
        self.origin = "http://127.0.0.1:" + str(self.port)

    def passed(self, name):
        self.checks.append(name)

    def request(self, path, body=None, headers=None):
        connection = http.client.HTTPConnection("127.0.0.1", self.port,
                                                 timeout=REQUEST_TIMEOUT)
        request_headers = {"Accept": "application/json"}
        request_headers.update(headers or {})
        encoded = None if body is None else json.dumps(body).encode("utf-8")
        if encoded is not None:
            request_headers["Content-Type"] = "application/json"
        try:
            connection.request("GET" if body is None else "POST", path,
                               body=encoded, headers=request_headers)
            response = connection.getresponse()
            raw = response.read(MAX_RESPONSE + 1)
            require(len(raw) <= MAX_RESPONSE)
            try:
                parsed = json.loads(raw) if raw else None
            except (ValueError, UnicodeDecodeError):
                parsed = None
            return response.status, dict(response.getheaders()), parsed
        finally:
            connection.close()

    def successful(self, path, body=None, headers=None):
        status, _, parsed = self.request(path, body, headers)
        require(status == 200 and isinstance(parsed, (dict, list)))
        if isinstance(parsed, dict):
            require("error" not in parsed)
            require(parsed.get("status", "success") == "success")
            return parsed.get("data", parsed)
        return parsed

    def rpc(self, method, params):
        status, _, body = self.request("/api/rpc2", {
            "jsonrpc": "2.0", "id": 1, "method": method, "params": params,
        }, self.auth)
        require(status == 200 and isinstance(body, dict))
        require(body.get("jsonrpc") == "2.0" and body.get("id") == 1)
        require("error" not in body and "result" in body)
        return body["result"]

    def wait_for(self, check, timeout=35):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            for process in (self.server, self.agent):
                if process is not None and process.poll() is not None:
                    raise ChildProcessError("isolated process exited")
            try:
                result = check()
                if result:
                    return result
            except (OSError, ValueError, http.client.HTTPException, AssertionError):
                pass
            time.sleep(0.2)
        raise TimeoutError("bounded wait expired")

    def start_server(self):
        self.server = subprocess.Popen([
            str(self.server_binary), "server", "--listen", "127.0.0.1:" + str(self.port),
            "--database", str(self.root / "primary.db"),
        ], cwd=str(self.root), env={"PATH": os.defpath, "TZ": "UTC"},
            stdin=subprocess.DEVNULL, stdout=self.logs["server"], stderr=self.logs["server"])

    def start_agent(self):
        self.agent = subprocess.Popen([
            str(self.agent_binary), "--config", str(self.root / "agent.json"),
        ], cwd=str(self.root), env={"PATH": os.defpath, "TZ": "UTC"},
            stdin=subprocess.DEVNULL, stdout=self.logs["agent"], stderr=self.logs["agent"])
        require(self.agent.poll() is None)
        running_executable = Path("/proc") / str(self.agent.pid) / "exe"
        require(sha256(running_executable if running_executable.exists()
                       else self.agent_binary) == self.agent_hash)

    def stop_agent(self):
        require(stop_process(self.agent))
        self.agent = None

    def stop_server(self):
        require(stop_process(self.server))
        self.server = None

    def version(self):
        version = self.successful("/api/version")
        require(isinstance(version, dict) and version.get("version") == "1.6.0")
        require(version.get("product") == "ShellBeam Server")
        require(version.get("upstream_version") == "1.6.0")
        for key in ("product_version", "upstream_commit"):
            require(isinstance(version.get(key), str) and bool(version[key]))
        return version

    def node(self):
        nodes = self.successful("/api/admin/client/list", headers=self.auth)
        require(isinstance(nodes, list) and len(nodes) == 1)
        require(nodes[0].get("uuid") == self.node_id)
        return nodes[0]

    def node_token(self):
        data = self.rpc("admin:getClientToken", {"uuid": self.node_id})
        require(isinstance(data, dict))
        token = data.get("token")
        require(isinstance(token, str) and bool(token))
        return token

    def latest(self):
        latest = self.rpc("common:getNodesLatestStatus", {"uuid": self.node_id})
        require(isinstance(latest, dict) and latest.get("client") == self.node_id)
        return latest

    def websocket_connections(self):
        # These messages are emitted only after the actual Agent's v2 WS
        # handshake succeeds. No simulated report or extra Agent socket is used.
        with open(self.root / "agent.log", "rb") as log:
            data = log.read(MAX_RESPONSE + 1)
        require(len(data) <= MAX_RESPONSE)
        return (data.count(b"WebSocket connected using v2 protocol")
                + data.count(b"WebSocket recovered from POST fallback"))

    def raw_samples(self):
        data = self.rpc("public:queryMetrics", {
            "metric_key": "cpu.usage", "entity_id": self.node_id,
            "hours": 0.1, "fill_empty": False, "max_points": 1000,
        })
        require(isinstance(data, dict) and isinstance(data.get("series"), list))
        points = []
        for series in data["series"]:
            if series.get("metric_key") == "cpu.usage" and series.get("entity_id") == self.node_id:
                require(series.get("downsampled") is False)
                require(series.get("fill_empty", False) is False)
                require(series.get("points") is None or isinstance(series["points"], list))
                points.extend(series.get("points") or [])
        for point in points:
            require(isinstance(point, dict))
            require(isinstance(point.get("value"), (int, float)))
            require(math.isfinite(point["value"]) and 0 <= point["value"] <= 100)
            timestamp(point.get("time"))
        return points

    def fresh_samples(self, after, minimum=3):
        require(self.node().get("version") == self.expected_version)
        latest = self.latest()
        require(latest.get("online") is True and timestamp(latest.get("time")) > after)
        require(self.websocket_connections() > 0)
        recent = self.successful("/api/recent/" + quote(self.node_id, safe=""), headers=self.auth)
        require(isinstance(recent, list))
        recent_by_time = {}
        for report in recent:
            require(isinstance(report, dict) and report.get("uuid") == self.node_id)
            at = timestamp(report.get("updated_at"))
            cpu = report.get("cpu", {}).get("usage")
            require(isinstance(cpu, (int, float)) and math.isfinite(cpu) and 0 <= cpu <= 100)
            if at > after:
                recent_by_time[timestamp_millis(report["updated_at"])] = cpu
        points = self.raw_samples()
        matching = [point for point in points
                    if timestamp_millis(point["time"]) in recent_by_time
                    and math.isclose(point["value"], recent_by_time[timestamp_millis(point["time"])],
                                     rel_tol=1e-9, abs_tol=1e-9)]
        unique_times = {timestamp(point["time"]) for point in matching}
        return max(unique_times) if len(unique_times) >= minimum else None

    def run(self):
        self.stage = "isolated-server-install"
        self.start_server()
        self.wait_for(lambda: self.successful("/api/install/status").get("required") is True)
        username, password = "smoke-" + secrets.token_hex(8), "Sb1" + secrets.token_hex(24)
        api_key = secrets.token_hex(32)
        self.successful("/api/install/complete", {
            "username": username, "password": password, "sitename": "Private Agent smoke test",
            "description": "Local-only real Agent compatibility check",
            "metric_dsn": str(self.root / "metrics.db"),
        })
        initial_version = self.wait_for(self.version)
        self.passed("server-api-1.6.0-and-shellbeam-product-metadata")

        self.stage = "isolated-node-and-private-config"
        status, headers, _ = self.request("/api/login", {"username": username, "password": password})
        require(status == 200)
        cookies = SimpleCookie()
        cookies.load(next((value for key, value in headers.items()
                           if key.lower() == "set-cookie"), ""))
        require("session_token" in cookies and bool(cookies["session_token"].value))
        session = {"Cookie": "session_token=" + cookies["session_token"].value,
                   "Origin": self.origin}
        self.successful("/api/admin/settings/", {
            "api_key": api_key, "private_site": True, "geo_ip_enabled": False,
            "notification_enabled": False, "login_notification": False, "eula_accepted": True,
        }, session)
        self.auth = {"Authorization": "Bearer " + api_key}
        require(self.successful("/api/admin/client/list", headers=self.auth) == [])
        created = self.successful("/api/admin/client/add", {"name": "real-agent-smoke-node"}, self.auth)
        require(isinstance(created, dict))
        self.node_id, token = created.get("uuid"), created.get("token")
        require(isinstance(self.node_id, str) and bool(self.node_id))
        require(isinstance(token, str) and bool(token))
        original = self.node()
        require(bool(original.get("created_at")) and self.node_token() == token)
        config = {
            "token": token, "endpoint": self.origin,
            "disable_auto_update": True, "disable_web_ssh": True,
            "custom_ipv4": "192.0.2.10", "custom_ipv6": "2001:db8::10",
            "interval": 1, "info_report_interval": 1,
            "max_retries": 1, "reconnect_interval": 1,
        }
        fd = os.open(str(self.root / "agent.json"), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as private_config:
            json.dump(config, private_config)
        self.passed("fresh-node-and-private-legacy-compatible-config")

        self.stage = "real-agent-websocket-and-original-raw-reports"
        started_at = time.time()
        self.start_agent()
        first_highwater = self.wait_for(lambda: self.fresh_samples(started_at))
        self.passed("real-agent-expected-version-and-v2-websocket-online")
        self.passed("three-original-cpu-samples-match-recent-timestamps-and-values")
        second_highwater = self.wait_for(lambda: self.fresh_samples(first_highwater))
        require(second_highwater > first_highwater)
        self.passed("original-raw-sample-times-continue-to-grow")

        self.stage = "agent-restart-and-token-continuity"
        ws_count = self.websocket_connections()
        self.stop_agent()
        self.wait_for(lambda: self.latest().get("online") is False)
        restarted_agent_at = time.time()
        self.start_agent()
        self.wait_for(lambda: self.fresh_samples(restarted_agent_at))
        require(self.websocket_connections() > ws_count)
        self.passed("agent-restart-offline-online-and-new-original-reports")
        for key in ("uuid", "created_at", "name"):
            require(self.node().get(key) == original.get(key))
        require(self.node_token() == token)
        self.passed("agent-restart-preserves-node-and-token")

        self.stage = "server-restart-and-live-agent-reconnect"
        agent_pid, ws_count = self.agent.pid, self.websocket_connections()
        self.stop_server()
        restarted_server_at = time.time()
        self.start_server()
        self.stage = "server-restart-product-and-installed-settings"
        require(self.wait_for(self.version) == initial_version)
        # The install API belongs to a temporary first-run listener. Verify the
        # normal authenticated router and saved settings after restart instead.
        settings = self.successful("/api/admin/settings/", headers=self.auth)
        require(isinstance(settings, dict) and settings.get("private_site") is True)
        require(settings.get("notification_enabled") is False)
        self.stage = "server-restart-persisted-node-identity"
        for key in ("uuid", "created_at", "name"):
            require(self.node().get(key) == original.get(key))
        require(self.node_token() == token)
        self.stage = "server-restart-existing-agent-fresh-reports"
        self.wait_for(lambda: self.fresh_samples(restarted_server_at))
        self.stage = "server-restart-existing-agent-websocket-reconnect"
        require(self.agent.pid == agent_pid and self.websocket_connections() > ws_count)
        self.passed("server-restart-preserves-node-token-and-api-key")
        self.passed("existing-agent-process-reconnects-with-fresh-original-reports")
        require(sha256(self.agent_binary) == self.agent_hash)
        self.passed("candidate-agent-binary-remains-unchanged")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--server-binary", required=True, type=Path)
    parser.add_argument("--agent-binary", required=True, type=Path)
    parser.add_argument("--expected-version", default="1.5.12")
    args = parser.parse_args()
    if sys.platform.startswith("linux") and os.geteuid() == 0:
        parser.error("run as an unprivileged user; the Agent may inspect Linux MOTD warning files")
    binaries = []
    for name in ("server_binary", "agent_binary"):
        path = getattr(args, name)
        if not path.is_absolute():
            parser.error("--" + name.replace("_", "-") + " must be an absolute path")
        try:
            path = path.resolve(strict=True)
            require(path.is_file() and os.access(path, os.X_OK))
        except (OSError, AssertionError):
            parser.error("--" + name.replace("_", "-") + " must identify an executable file")
        binaries.append(path)

    smoke, failure = None, None
    cleanup = {"agent_stopped": True, "server_stopped": True, "temporary_state_removed": False}
    temp = tempfile.TemporaryDirectory(prefix="shellbeam-real-agent-smoke-")
    root = Path(temp.name)
    os.chmod(root, 0o700)
    handlers, logs = {}, {}

    def interrupted(_signum, _frame):
        raise InterruptedError("interrupted")

    try:
        for signum in (signal.SIGINT, signal.SIGTERM):
            handlers[signum] = signal.signal(signum, interrupted)
        for name in ("agent", "server"):
            fd = os.open(str(root / (name + ".log")), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            logs[name] = os.fdopen(fd, "wb", buffering=0)
        try:
            smoke = Smoke(*binaries, args.expected_version, root, logs)
            smoke.run()
        except (Exception, KeyboardInterrupt) as error:
            failure = {"stage": smoke.stage if smoke else "initialization", "type": type(error).__name__}
        finally:
            if smoke is not None:
                for name in ("agent", "server"):
                    try:
                        cleanup[name + "_stopped"] = stop_process(getattr(smoke, name))
                    except Exception as error:
                        cleanup[name + "_stopped"] = False
                        failure = failure or {"stage": "cleanup-" + name, "type": type(error).__name__}
    finally:
        for log in logs.values():
            log.close()
        for signum, handler in handlers.items():
            signal.signal(signum, handler)
        if cleanup["agent_stopped"] and cleanup["server_stopped"]:
            try:
                temp.cleanup()
                cleanup["temporary_state_removed"] = not root.exists()
                require(cleanup["temporary_state_removed"])
            except Exception as error:
                failure = failure or {"stage": "cleanup-directory", "type": type(error).__name__}
        else:
            # Keep private state only when the OS cannot confirm process exit.
            temp._finalizer.detach()
    checks = smoke.checks if smoke else []
    summary = {"result": "failed" if failure else "passed", "checks": checks,
               "count": len(checks), "cleanup": cleanup}
    if failure:
        summary["failure"] = failure
    print(json.dumps(summary, separators=(",", ":")))
    return 1 if failure else 0


if __name__ == "__main__":
    sys.exit(main())
