"""Tests for the ``agentguard-codex`` entry point.

These cover the module wiring the dispatch tests miss: ``cli.py`` imports and
``python -m agentguard_codex`` runs. They do not need the AgentGuard SDK or a
collector — a missing SDK is one of the failures the hook must swallow.
"""

import json
import os
import socket
import subprocess
import sys
import tempfile
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


class ResolveTests(unittest.TestCase):
    """Flag beats env beats default, for every configurable value."""

    ENV_VARS = (
        "AGENTGUARD_AGENT_ID",
        "AGENTGUARD_DEPLOYMENT_ID",
        "AGENTGUARD_DEPLOYMENT_NAME",
        "OTEL_EXPORTER_OTLP_ENDPOINT",
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


if __name__ == "__main__":
    unittest.main()
