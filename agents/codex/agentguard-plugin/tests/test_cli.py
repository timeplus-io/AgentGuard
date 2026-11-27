"""Tests for the ``agentguard-codex`` entry point.

These cover the module wiring the dispatch tests miss: ``cli.py`` imports and
``python -m agentguard_codex`` runs. Most do not need the AgentGuard SDK or a
collector — a missing SDK is one of the failures the hook must swallow.
``TelemetryTests`` does need the SDK (it is skipped without it) and exports to
an in-process OTLP/HTTP receiver.
"""

import contextlib
import http.server
import importlib.util
import io
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


class CliTests(unittest.TestCase):
    def test_cli_module_imports(self) -> None:
        # Regression guard: a broken import in cli.py fails every hook, while
        # the events-only tests keep passing.
        from agentguard_codex import cli

        self.assertTrue(callable(cli.main))

    def test_help_exits_zero(self) -> None:
        result = subprocess.run(
            [sys.executable, "-m", "agentguard_codex", "--help"],
            cwd=PLUGIN_ROOT,
            capture_output=True,
            text=True,
            timeout=30,
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("agentguard-codex", result.stdout)

    def test_module_entry_point_never_fails_the_hook(self) -> None:
        event = {"hook_event_name": "SessionStart", "session_id": "s1"}
        with tempfile.TemporaryDirectory() as home:
            env = {**os.environ, "HOME": home}
            result = subprocess.run(
                [sys.executable, "-m", "agentguard_codex", "--agent-id", "test-agent"],
                cwd=PLUGIN_ROOT,
                input=json.dumps(event),
                capture_output=True,
                text=True,
                env=env,
                timeout=30,
            )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn("Traceback", result.stderr)


class _Receiver(http.server.BaseHTTPRequestHandler):
    """Minimal OTLP/HTTP trace receiver: keeps every request body."""

    def do_POST(self) -> None:  # noqa: N802 - http.server API
        body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        self.server.bodies.append((self.path, body))
        self.send_response(200)
        self.send_header("Content-Type", "application/x-protobuf")
        self.send_header("Content-Length", "0")
        self.end_headers()

    def log_message(self, *args) -> None:
        pass


class _ReceiverServer(http.server.ThreadingHTTPServer):
    def server_bind(self) -> None:
        # HTTPServer.server_bind also resolves the host's FQDN, a reverse DNS
        # lookup that can take tens of seconds; the tests never need the name.
        socketserver.TCPServer.server_bind(self)


@unittest.skipUnless(
    importlib.util.find_spec("agentguard"), "needs the AgentGuard SDK (timeplus-agentguard)"
)
class TelemetryTests(unittest.TestCase):
    """Run the real hook command and decode what it exports."""

    # One full session, in the order Codex fires the hooks.
    COMMON = {
        "session_id": "s1",
        "transcript_path": None,
        "cwd": "/work/demo-repo",
        "model": "gpt-5-codex",
    }
    TURN = {**COMMON, "turn_id": "t1"}
    MODEL = {**TURN, "request_id": "req_1", "attempt_number": 1, "kind": "sampling"}
    SUBAGENT = {**TURN, "agent_id": "agent_7", "agent_type": "explorer"}
    EVENTS = [
        {**COMMON, "hook_event_name": "SessionStart", "source": "startup"},
        {**TURN, "hook_event_name": "UserPromptSubmit", "prompt": "fix the test"},
        {**MODEL, "hook_event_name": "PreModelRequest"},
        {
            **MODEL,
            "hook_event_name": "PostModelRequest",
            "status": "failed",
            "error": "stream disconnected",
            "token_usage": None,
        },
        {**MODEL, "hook_event_name": "PreModelRequest", "attempt_number": 2},
        {
            **MODEL,
            "hook_event_name": "PostModelRequest",
            "attempt_number": 2,
            "status": "completed",
            "error": None,
            "token_usage": {
                "input_tokens": 1200,
                "cached_input_tokens": 800,
                "output_tokens": 300,
                "reasoning_output_tokens": 120,
                "total_tokens": 1500,
            },
        },
        {
            **TURN,
            "hook_event_name": "PermissionRequest",
            "tool_name": "Bash",
            "tool_input": {"command": "make test", "description": "Run the tests"},
        },
        {**TURN, "hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_use_id": "c1"},
        {
            **TURN,
            "hook_event_name": "PostToolUse",
            "tool_name": "Bash",
            "tool_use_id": "c1",
            "tool_input": {"command": "make test"},
            "tool_response": {"exit_code": 0},
        },
        {**SUBAGENT, "hook_event_name": "SubagentStart"},
        {
            **SUBAGENT,
            "hook_event_name": "SubagentStop",
            "agent_transcript_path": None,
            "stop_hook_active": False,
            "last_assistant_message": "Found it.",
        },
        {**TURN, "hook_event_name": "PreCompact", "trigger": "auto"},
        {**TURN, "hook_event_name": "PostCompact", "trigger": "auto"},
        {
            **TURN,
            "hook_event_name": "Stop",
            "stop_hook_active": False,
            "last_assistant_message": "Fixed.",
        },
        {**TURN, "hook_event_name": "Interrupt"},
        {**COMMON, "hook_event_name": "SessionEnd", "reason": "other"},
    ]

    @classmethod
    def setUpClass(cls) -> None:
        from opentelemetry.proto.collector.trace.v1.trace_service_pb2 import (
            ExportTraceServiceRequest,
        )

        server = _ReceiverServer(("127.0.0.1", 0), _Receiver)
        server.bodies = []
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        cls.addClassCleanup(thread.join)
        cls.addClassCleanup(server.server_close)
        cls.addClassCleanup(server.shutdown)

        cls.results = []
        with tempfile.TemporaryDirectory() as home:
            env = {**os.environ, "HOME": home}
            env.pop("OTEL_EXPORTER_OTLP_ENDPOINT", None)
            for event in cls.EVENTS:
                cls.results.append(
                    subprocess.run(
                        [
                            sys.executable,
                            "-m",
                            "agentguard_codex",
                            "--agent-id",
                            "test-agent",
                            "--endpoint",
                            f"http://127.0.0.1:{server.server_address[1]}",
                        ],
                        cwd=PLUGIN_ROOT,
                        input=json.dumps(event),
                        capture_output=True,
                        text=True,
                        env=env,
                        timeout=60,
                    )
                )

        cls.spans = []
        cls.resources = []
        for path, body in server.bodies:
            assert path == "/v1/traces", path
            request = ExportTraceServiceRequest()
            request.ParseFromString(body)
            for resource_spans in request.resource_spans:
                cls.resources.append(cls.attributes(resource_spans.resource))
                for scope_spans in resource_spans.scope_spans:
                    cls.spans.extend(scope_spans.spans)

    @staticmethod
    def attributes(span) -> dict:
        """Attributes of a span or a resource as a plain dict."""
        out = {}
        for item in span.attributes:
            out[item.key] = getattr(item.value, item.value.WhichOneof("value"))
        return out

    def named(self, name: str) -> list:
        return [span for span in self.spans if span.name == name]

    def test_hooks_stay_observe_only(self) -> None:
        # Exit 0 with empty stdout: Codex reads stdout as a decision or as
        # context, so anything printed there could change the turn.
        for event, result in zip(self.EVENTS, self.results):
            with self.subTest(event=event["hook_event_name"]):
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout, "")
                self.assertNotIn("Traceback", result.stderr)

    def test_one_span_per_event(self) -> None:
        counts: dict[str, int] = {}
        for span in self.spans:
            counts[span.name] = counts.get(span.name, 0) + 1

        self.assertEqual(
            counts,
            {
                "session_start": 1,
                "session": 1,
                "user_prompt_submit": 1,
                "tool": 1,  # PreToolUse only records the start time
                "pre_model_request": 2,
                "post_model_request": 2,
                "permission_request": 1,
                "subagent_start": 1,
                "subagent_stop": 1,
                "pre_compact": 1,
                "post_compact": 1,
                "stop": 1,
                "interrupt": 1,
            },
        )

    def test_agent_type_is_codex(self) -> None:
        # The app reports the OTel service name as agent_type.
        self.assertTrue(self.resources)
        for resource in self.resources:
            self.assertEqual(resource["service.name"], "codex")
            self.assertEqual(resource["agent.id"], "test-agent")

    def test_every_span_carries_session_cwd_and_model(self) -> None:
        for span in self.spans:
            attributes = self.attributes(span)
            with self.subTest(span=span.name):
                self.assertEqual(attributes["session.id"], "s1")
                self.assertEqual(attributes["cwd"], "/work/demo-repo")
                self.assertEqual(attributes["llm.model_name"], "gpt-5-codex")
                self.assertEqual(attributes["llm.provider"], "openai")
                if span.name not in ("session", "session_start"):
                    self.assertEqual(attributes["run.id"], "t1")

    def test_session_start_is_exported_before_the_session_ends(self) -> None:
        (start,) = self.named("session_start")
        (session,) = self.named("session")

        self.assertEqual(self.attributes(start)["source"], "startup")
        self.assertNotIn("agentguard.span.kind", self.attributes(start))
        self.assertEqual(self.attributes(session)["agentguard.span.kind"], "SESSION")
        # The session span runs from SessionStart to SessionEnd.
        self.assertLessEqual(session.start_time_unix_nano, start.start_time_unix_nano)
        for span in self.spans:
            self.assertGreaterEqual(session.end_time_unix_nano, span.end_time_unix_nano)

    def test_prompt_and_reply_text(self) -> None:
        (prompt,) = self.named("user_prompt_submit")
        self.assertEqual(
            json.loads(self.attributes(prompt)["input.value"]),
            {"messages": [{"role": "user", "content": "fix the test"}]},
        )
        # A prompt is not an LLM call: it takes no time and has no usage.
        self.assertNotIn("openinference.span.kind", self.attributes(prompt))

        (stop,) = self.named("stop")
        self.assertEqual(self.attributes(stop)["output.value"], "Fixed.")

    def test_tool_span(self) -> None:
        (tool,) = self.named("tool")
        attributes = self.attributes(tool)

        self.assertEqual(attributes["openinference.span.kind"], "TOOL")
        self.assertEqual(attributes["tool.name"], "Bash")
        self.assertEqual(json.loads(attributes["tool.parameters"]), {"command": "make test"})
        self.assertEqual(attributes["tool_use_id"], "c1")
        self.assertGreater(tool.end_time_unix_nano - tool.start_time_unix_nano, 1_000_000)

    def test_model_attempts_carry_status_and_usage(self) -> None:
        failed, completed = sorted(
            self.named("post_model_request"),
            key=lambda span: self.attributes(span)["codex.request.attempt"],
        )

        failed_attributes = self.attributes(failed)
        self.assertEqual(failed_attributes["codex.request.status"], "failed")
        self.assertEqual(failed_attributes["codex.request.error"], "stream disconnected")
        self.assertEqual(failed.status.code, 2)  # STATUS_CODE_ERROR
        self.assertNotIn("llm.token_count.total", failed_attributes)

        completed_attributes = self.attributes(completed)
        self.assertEqual(completed_attributes["codex.request.status"], "completed")
        self.assertEqual(completed_attributes["llm.token_count.prompt"], 1200)
        self.assertEqual(completed_attributes["llm.token_count.completion"], 300)
        self.assertEqual(completed_attributes["llm.token_count.total"], 1500)
        self.assertNotEqual(completed.status.code, 2)

    def test_llm_calls_are_model_requests_only(self) -> None:
        llm_kind = [
            s.name
            for s in self.spans
            if self.attributes(s).get("openinference.span.kind") == "LLM"
        ]
        # One per attempt, none for the prompt or the final reply.
        self.assertEqual(llm_kind, ["post_model_request", "post_model_request"])

        usage = [s.name for s in self.spans if "llm.token_count.total" in self.attributes(s)]
        self.assertEqual(usage, ["post_model_request"])

        responses = [s.name for s in self.spans if "output.value" in self.attributes(s)]
        self.assertEqual(sorted(responses), ["stop", "subagent_stop", "tool"])

    def test_post_spans_start_at_their_pre_event(self) -> None:
        pairs = [
            ("pre_compact", "post_compact"),
            ("subagent_start", "subagent_stop"),
        ]
        for pre_name, post_name in pairs:
            with self.subTest(pair=post_name):
                (pre,) = self.named(pre_name)
                (post,) = self.named(post_name)
                self.assertLessEqual(post.start_time_unix_nano, pre.start_time_unix_nano)
                self.assertGreater(post.end_time_unix_nano, pre.end_time_unix_nano)

        for attempt in (1, 2):
            with self.subTest(pair="post_model_request", attempt=attempt):
                pre, post = (
                    next(
                        span
                        for span in self.named(name)
                        if self.attributes(span)["codex.request.attempt"] == attempt
                    )
                    for name in ("pre_model_request", "post_model_request")
                )
                self.assertLessEqual(post.start_time_unix_nano, pre.start_time_unix_nano)

    def test_subagent_is_correlated_with_the_parent_session(self) -> None:
        for name in ("subagent_start", "subagent_stop"):
            (span,) = self.named(name)
            attributes = self.attributes(span)
            self.assertEqual(attributes["session.id"], "s1")
            self.assertEqual(attributes["agentguard.subagent.id"], "agent_7")
            self.assertEqual(attributes["agentguard.subagent.type"], "explorer")

    def test_only_the_session_span_is_marked_as_a_session(self) -> None:
        marked = [s.name for s in self.spans if "agentguard.span.kind" in self.attributes(s)]
        self.assertEqual(marked, ["session"])


class ResolveTests(unittest.TestCase):
    """Flag beats env beats default, for every configurable value."""

    ENV_VARS = (
        "AGENTGUARD_AGENT_ID",
        "AGENTGUARD_DEPLOYMENT_ID",
        "AGENTGUARD_DEPLOYMENT_NAME",
        "OTEL_EXPORTER_OTLP_ENDPOINT",
        "AGENTGUARD_CODEX_EXPORT",
    )

    def setUp(self) -> None:
        from agentguard_codex import cli

        self.cli = cli
        self._saved = {name: os.environ.pop(name, None) for name in self.ENV_VARS}

    def tearDown(self) -> None:
        for name, value in self._saved.items():
            if value is None:
                os.environ.pop(name, None)
            else:
                os.environ[name] = value

    def test_flag_wins_over_env(self) -> None:
        os.environ["AGENTGUARD_AGENT_ID"] = "env-agent"
        os.environ["AGENTGUARD_DEPLOYMENT_ID"] = "env-deployment"
        os.environ["AGENTGUARD_DEPLOYMENT_NAME"] = "Env Deployment"
        os.environ["OTEL_EXPORTER_OTLP_ENDPOINT"] = "http://env:4318"

        self.assertEqual(self.cli.resolve_agent_id("flag-agent"), "flag-agent")
        self.assertEqual(self.cli.resolve_deployment_id("flag-deployment"), "flag-deployment")
        self.assertEqual(self.cli.resolve_deployment_name("Flag Deployment"), "Flag Deployment")
        self.assertEqual(self.cli.resolve_endpoint("http://flag:4318"), "http://flag:4318")

    def test_env_wins_over_default(self) -> None:
        os.environ["AGENTGUARD_AGENT_ID"] = "env-agent"
        os.environ["AGENTGUARD_DEPLOYMENT_ID"] = "env-deployment"
        os.environ["AGENTGUARD_DEPLOYMENT_NAME"] = "Env Deployment"
        os.environ["OTEL_EXPORTER_OTLP_ENDPOINT"] = "http://env:4318"

        self.assertEqual(self.cli.resolve_agent_id(None), "env-agent")
        self.assertEqual(self.cli.resolve_deployment_id(None), "env-deployment")
        self.assertEqual(self.cli.resolve_deployment_name(None), "Env Deployment")
        self.assertEqual(self.cli.resolve_endpoint(None), "http://env:4318")

    def test_empty_env_falls_through_to_default(self) -> None:
        for name in self.ENV_VARS:
            os.environ[name] = ""

        self.assertEqual(self.cli.resolve_agent_id(None), socket.gethostname())
        self.assertEqual(self.cli.resolve_deployment_id(None), self.cli.DEFAULT_DEPLOYMENT_ID)
        self.assertEqual(self.cli.resolve_deployment_name(None), self.cli.DEFAULT_DEPLOYMENT_NAME)
        self.assertEqual(self.cli.resolve_endpoint(None), self.cli.DEFAULT_ENDPOINT)

    def test_defaults(self) -> None:
        self.assertEqual(self.cli.resolve_deployment_id(None), "local")
        self.assertEqual(self.cli.resolve_deployment_name(None), "Local Dev")
        self.assertEqual(self.cli.resolve_endpoint(None), "http://localhost:4318")

    def test_export_defaults_to_otel(self) -> None:
        self.assertEqual(self.cli.resolve_export(None), "otel")
        os.environ["AGENTGUARD_CODEX_EXPORT"] = ""
        self.assertEqual(self.cli.resolve_export(None), "otel")

    def test_export_flag_wins_over_env(self) -> None:
        os.environ["AGENTGUARD_CODEX_EXPORT"] = "otel"
        self.assertEqual(self.cli.resolve_export("hooks"), "hooks")
        os.environ["AGENTGUARD_CODEX_EXPORT"] = " Hooks "
        self.assertEqual(self.cli.resolve_export(None), "hooks")

    def test_unknown_export_falls_back_to_otel_and_says_so(self) -> None:
        # No value selects both paths: "both" is as unknown as a typo.
        for value in ("both", "otel,hooks", "hook"):
            with self.subTest(value=value):
                stderr = io.StringIO()
                with contextlib.redirect_stderr(stderr):
                    self.assertEqual(self.cli.resolve_export(value), "otel")
                self.assertIn(value, stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
