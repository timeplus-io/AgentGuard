"""Tests for the ``agentguard-codex`` entry point.

These cover the module wiring the dispatch tests miss: ``cli.py`` imports and
``python -m agentguard_codex`` runs. They do not need the AgentGuard SDK or a
collector — a missing SDK is one of the failures the hook must swallow.
"""

import json
import os
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


if __name__ == "__main__":
    unittest.main()
