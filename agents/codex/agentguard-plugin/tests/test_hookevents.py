"""Tests for the hook-event export path (``--export hooks``).

Standard library only: this path must work without the AgentGuard SDK.

Run:  python3.12 -m unittest discover -s tests -v
"""

import base64
import http.server
import json
import os
import socket
import socketserver
import subprocess
import sys
import tempfile
import threading
import unittest

PLUGIN_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, PLUGIN_ROOT)
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_cli  # noqa: E402

from agentguard_codex.events import SUPPORTED_EVENTS, dispatch  # noqa: E402
from agentguard_codex.hookevents import COLUMNS, HookEventSink, IngestConfig  # noqa: E402

#: One full session covering all 14 hooks, shared with the OTel path tests.
EVENTS = test_cli.TelemetryTests.EVENTS

#: The app's agentguard_hook_events columns (apps/agentguard/ddl in timeplus-io/apps).
STREAM_COLUMNS = [
    "hook_name",
    "event_time",
    "session_id",
    "run_id",
    "agent_id",
    "deployment_id",
    "deployment_name",
    "session_key",
    "tool_name",
    "provider",
    "model",
    "hook_decision",
    "block_reason",
    "event_data",
    "agent_type",
]

#: hook_name written for each Codex hook.
HOOK_NAMES = {
    "SessionStart": "session_start",
    "SessionEnd": "session_end",
    "UserPromptSubmit": "user_prompt_submit",
    "PreToolUse": "pre_tool_use",
    "PostToolUse": "post_tool_use",
    "PreModelRequest": "pre_model_request",
    "PostModelRequest": "post_model_request",
    "Stop": "stop",
    "Interrupt": "interrupt",
    "SubagentStart": "subagent_start",
    "SubagentStop": "subagent_ended",
    "PermissionRequest": "permission_request",
    "PreCompact": "pre_compact",
    "PostCompact": "post_compact",
}


class MemoryStarts:
    def __init__(self) -> None:
        self.values: dict[str, int] = {}

    def mark(self, key: str) -> None:
        self.values[key] = 1_000  # long ago, so every duration is positive

    def take(self, key: str) -> int | None:
        return self.values.pop(key, None)


def as_dicts(rows: list[list]) -> list[dict]:
    return [dict(zip(COLUMNS, row)) for row in rows]


class IngestConfigTests(unittest.TestCase):
    def test_defaults_match_the_other_plugins(self) -> None:
        config = IngestConfig.from_env({})

        self.assertEqual(
            config.ingest_url(),
            "http://localhost:3218/timeplusd/v2/ingest/ag/streams/agentguard_hook_events",
        )

    def test_rest_mode(self) -> None:
        config = IngestConfig.from_env(
            {"AGENTGUARD_TIMEPLUS_URL": "http://tp:8000/", "AGENTGUARD_INGEST_API": " REST "}
        )

        self.assertEqual(
            config.ingest_url(),
            "http://tp:8000/default/api/v1beta2/streams/agentguard_hook_events/ingest?db=ag",
        )

    def test_unknown_ingest_api_is_timeplusd(self) -> None:
        self.assertEqual(
            IngestConfig.from_env({"AGENTGUARD_INGEST_API": "cloud"}).ingest_api, "timeplusd"
        )

    def test_env_overrides(self) -> None:
        config = IngestConfig.from_env(
            {
                "AGENTGUARD_TIMEPLUS_URL": "http://tp:3218",
                "AGENTGUARD_DATABASE": "db1",
                "AGENTGUARD_STREAM": "s1",
                "AGENTGUARD_WORKSPACE": "ws1",
                "AGENTGUARD_USERNAME": "alice",
                "AGENTGUARD_PASSWORD": "secret",
            }
        )

        self.assertEqual(config.ingest_url(), "http://tp:3218/timeplusd/v2/ingest/db1/streams/s1")
        token = config.headers()["Authorization"].removeprefix("Basic ")
        self.assertEqual(base64.b64decode(token).decode(), "alice:secret")

    def test_empty_env_values_fall_back_to_defaults(self) -> None:
        config = IngestConfig.from_env({"AGENTGUARD_TIMEPLUS_URL": "", "AGENTGUARD_DATABASE": ""})

        self.assertEqual(config, IngestConfig())


class RowTests(unittest.TestCase):
    """The HookEvent row each Codex hook becomes."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.sink = HookEventSink(
            IngestConfig(),
            MemoryStarts(),
            agent_id="agent-1",
            deployment_id="dep-1",
            deployment_name="Dep One",
        )
        for event in EVENTS:
            assert dispatch(event, cls.sink)
        cls.rows = as_dicts(cls.sink.rows)

    def row(self, hook_name: str) -> dict:
        (row,) = [r for r in self.rows if r["hook_name"] == hook_name]
        return {**row, "event_data": json.loads(row["event_data"])}

    def test_columns_match_the_stream(self) -> None:
        self.assertEqual(COLUMNS, STREAM_COLUMNS)
        for row in self.sink.rows:
            self.assertEqual(len(row), len(COLUMNS))
            for value in row:
                self.assertIsInstance(value, str)

    def test_one_row_per_hook_for_all_14_hooks(self) -> None:
        self.assertEqual({e["hook_event_name"] for e in EVENTS}, SUPPORTED_EVENTS)
        self.assertEqual(set(HOOK_NAMES), SUPPORTED_EVENTS)
        self.assertEqual(
            [r["hook_name"] for r in self.rows],
            [HOOK_NAMES[e["hook_event_name"]] for e in EVENTS],
        )

    def test_identity_columns(self) -> None:
        for row in self.rows:
            self.assertEqual(row["agent_type"], "codex")
            self.assertEqual(row["agent_id"], "agent-1")
            self.assertEqual(row["deployment_id"], "dep-1")
            self.assertEqual(row["deployment_name"], "Dep One")
            self.assertEqual(row["session_id"], "s1")
            self.assertEqual(row["hook_decision"], "observe")
            self.assertEqual(row["block_reason"], "")
            self.assertEqual(row["model"], "gpt-5-codex")
            self.assertEqual(row["provider"], "openai")

    def test_run_id_is_the_turn_or_else_the_session(self) -> None:
        self.assertEqual(self.row("user_prompt_submit")["run_id"], "t1")
        self.assertEqual(self.row("session_start")["run_id"], "s1")
        self.assertEqual(self.row("session_end")["run_id"], "s1")

    def test_event_data_is_the_payload_plus_the_path_marker(self) -> None:
        for event, row in zip(EVENTS, self.rows):
            data = json.loads(row["event_data"])
            with self.subTest(event=event["hook_event_name"]):
                self.assertEqual(data.pop("agentguard.export"), "hooks")
                data.pop("agentguard.duration_ms", None)
                self.assertEqual(data, event)

    def test_prompt_reply_and_working_directory(self) -> None:
        self.assertEqual(self.row("user_prompt_submit")["event_data"]["prompt"], "fix the test")
        self.assertEqual(self.row("stop")["event_data"]["last_assistant_message"], "Fixed.")
        self.assertEqual(self.row("session_start")["event_data"]["cwd"], "/work/demo-repo")
        self.assertEqual(self.row("session_start")["event_data"]["source"], "startup")

    def test_tool_rows(self) -> None:
        pre, post = self.row("pre_tool_use"), self.row("post_tool_use")

        self.assertEqual((pre["tool_name"], post["tool_name"]), ("Bash", "Bash"))
        self.assertEqual(post["event_data"]["tool_use_id"], "c1")
        self.assertEqual(post["event_data"]["tool_input"], {"command": "make test"})
        self.assertEqual(post["event_data"]["tool_response"], {"exit_code": 0})
        self.assertNotIn("agentguard.duration_ms", pre["event_data"])
        self.assertGreater(post["event_data"]["agentguard.duration_ms"], 0)

    def test_model_request_rows_carry_usage_and_duration(self) -> None:
        failed, completed = [
            json.loads(r["event_data"]) for r in self.rows if r["hook_name"] == "post_model_request"
        ]

        self.assertEqual(failed["status"], "failed")
        self.assertEqual(completed["token_usage"]["input_tokens"], 1200)
        self.assertEqual(completed["token_usage"]["cached_input_tokens"], 800)
        self.assertGreater(completed["agentguard.duration_ms"], 0)

    def test_subagent_and_compaction_keys(self) -> None:
        # The names Agent Rooms reads for these events.
        ended = self.row("subagent_ended")["event_data"]

        self.assertEqual((ended["agent_id"], ended["agent_type"]), ("agent_7", "explorer"))
        self.assertEqual(self.row("pre_compact")["event_data"]["trigger"], "auto")
        self.assertEqual(self.row("permission_request")["tool_name"], "Bash")


class _Receiver(http.server.BaseHTTPRequestHandler):
    def do_POST(self) -> None:  # noqa: N802 - http.server API
        body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        self.server.requests.append((self.path, dict(self.headers), body))
        self.send_response(200)
        self.send_header("Content-Length", "0")
        self.end_headers()

    def log_message(self, *args) -> None:
        pass


class _ReceiverServer(http.server.ThreadingHTTPServer):
    def server_bind(self) -> None:
        socketserver.TCPServer.server_bind(self)  # skip the reverse DNS lookup


def run_hook(event: dict, env: dict, *args: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        [sys.executable, "-m", "agentguard_codex", "--agent-id", "test-agent", *args],
        cwd=PLUGIN_ROOT,
        input=json.dumps(event),
        capture_output=True,
        text=True,
        env=env,
        timeout=60,
    )


class CommandTests(unittest.TestCase):
    """Run the real hook command against a local ingest endpoint."""

    def setUp(self) -> None:
        self.server = _ReceiverServer(("127.0.0.1", 0), _Receiver)
        self.server.requests = []
        thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(thread.join)
        self.addCleanup(self.server.server_close)
        self.addCleanup(self.server.shutdown)

        home = tempfile.TemporaryDirectory()
        self.addCleanup(home.cleanup)
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}"
        self.env = {
            k: v
            for k, v in os.environ.items()
            if not k.startswith("AGENTGUARD_") and not k.startswith("OTEL_")
        }
        self.env.update(
            HOME=home.name,
            AGENTGUARD_TIMEPLUS_URL=self.url,
            AGENTGUARD_USERNAME="alice",
            AGENTGUARD_PASSWORD="secret",
            # Any OTLP export would land here and fail the path check below.
            OTEL_EXPORTER_OTLP_ENDPOINT=self.url,
        )

    def run_session(self, *args: str) -> list[dict]:
        for event in EVENTS:
            result = run_hook(event, self.env, *args)
            with self.subTest(event=event["hook_event_name"]):
                # Observe only: exit 0 and nothing on stdout.
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout, "")
                self.assertEqual(result.stderr, "")
        rows = []
        for _, _, body in self.server.requests:
            payload = json.loads(body)
            self.assertEqual(payload["columns"], STREAM_COLUMNS)
            rows.extend(as_dicts(payload["data"]))
        return rows

    def test_timeplusd_ingest(self) -> None:
        rows = self.run_session("--export", "hooks")

        self.assertEqual(
            {path for path, _, _ in self.server.requests},
            {"/timeplusd/v2/ingest/ag/streams/agentguard_hook_events"},
        )
        self.assertEqual(
            [r["hook_name"] for r in rows], [HOOK_NAMES[e["hook_event_name"]] for e in EVENTS]
        )
        self.assertEqual({r["agent_type"] for r in rows}, {"codex"})
        self.assertEqual({r["agent_id"] for r in rows}, {"test-agent"})
        for _, headers, _ in self.server.requests:
            token = headers["Authorization"].removeprefix("Basic ")
            self.assertEqual(base64.b64decode(token).decode(), "alice:secret")

    def test_rest_ingest_selected_by_env(self) -> None:
        self.env.update(AGENTGUARD_CODEX_EXPORT="hooks", AGENTGUARD_INGEST_API="rest")

        rows = self.run_session()

        self.assertEqual(
            {path for path, _, _ in self.server.requests},
            {"/default/api/v1beta2/streams/agentguard_hook_events/ingest?db=ag"},
        )
        self.assertEqual(len(rows), len(EVENTS))

    def test_pre_and_post_events_are_paired_across_processes(self) -> None:
        rows = self.run_session("--export", "hooks")

        for name in ("post_tool_use", "post_model_request", "session_end", "post_compact"):
            for row in rows:
                if row["hook_name"] == name:
                    self.assertIn("agentguard.duration_ms", json.loads(row["event_data"]), name)

    def test_failed_ingest_never_changes_the_turn(self) -> None:
        with socket.socket() as closed:
            closed.bind(("127.0.0.1", 0))
            self.env["AGENTGUARD_TIMEPLUS_URL"] = f"http://127.0.0.1:{closed.getsockname()[1]}"

        for event in EVENTS:
            result = run_hook(event, self.env, "--export", "hooks")
            with self.subTest(event=event["hook_event_name"]):
                self.assertEqual(result.returncode, 0)
                self.assertEqual(result.stdout, "")
                self.assertIn("agentguard-codex:", result.stderr)
                self.assertNotIn("Traceback", result.stderr)
        self.assertEqual(self.server.requests, [])

    def test_unknown_export_does_not_use_the_hook_path(self) -> None:
        result = run_hook(EVENTS[0], self.env, "--export", "both")

        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")
        self.assertIn("unknown export 'both'", result.stderr)
        self.assertEqual(
            [p for p, _, _ in self.server.requests if "agentguard_hook_events" in p], []
        )


if __name__ == "__main__":
    unittest.main()
