"""Unit tests for Codex hook event translation.

The tests use a recording sink instead of the real AgentGuard SDK, so they
need no dependencies and no running collector.

Run:  python3.12 -m unittest discover -s tests -v
"""

import os
import sys
import unittest

PLUGIN_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, PLUGIN_ROOT)

from agentguard_codex.events import dispatch  # noqa: E402


class RecordingHooks:
    """Records every SDK call made by :func:`dispatch`."""

    def __init__(self) -> None:
        self.calls: list[tuple[str, tuple, dict]] = []

    def __getattr__(self, name: str):
        def record(*args, **kwargs):
            self.calls.append((name, args, kwargs))

        return record


class DispatchTests(unittest.TestCase):
    def setUp(self) -> None:
        self.hooks = RecordingHooks()

    def test_session_start(self) -> None:
        handled = dispatch({"hook_event_name": "SessionStart", "session_id": "s1"}, self.hooks)

        self.assertTrue(handled)
        self.assertEqual(self.hooks.calls, [("on_session_start", ("s1",), {})])

    def test_session_end(self) -> None:
        handled = dispatch({"hook_event_name": "SessionEnd", "session_id": "s1"}, self.hooks)

        self.assertTrue(handled)
        self.assertEqual(self.hooks.calls, [("on_session_end", ("s1",), {})])

    def test_user_prompt_submit(self) -> None:
        handled = dispatch(
            {
                "hook_event_name": "UserPromptSubmit",
                "session_id": "s1",
                "turn_id": "t1",
                "model": "gpt-5-codex",
                "prompt": "fix the failing test",
            },
            self.hooks,
        )

        self.assertTrue(handled)
        self.assertEqual(
            self.hooks.calls,
            [
                (
                    "on_llm_end",
                    (),
                    {
                        "session_id": "s1",
                        "task_id": "t1",
                        "model_name": "gpt-5-codex",
                        "user_message": "fix the failing test",
                    },
                )
            ],
        )

    def test_pre_tool_use(self) -> None:
        handled = dispatch(
            {
                "hook_event_name": "PreToolUse",
                "session_id": "s1",
                "turn_id": "t1",
                "tool_name": "Bash",
            },
            self.hooks,
        )

        self.assertTrue(handled)
        self.assertEqual(self.hooks.calls, [("on_pre_tool", (), {"session_id": "s1"})])

    def test_post_tool_use_stringifies_inputs(self) -> None:
        handled = dispatch(
            {
                "hook_event_name": "PostToolUse",
                "session_id": "s1",
                "turn_id": "t1",
                "tool_name": "Bash",
                "tool_input": {"command": "ls"},
                "tool_response": {"stdout": "README.md"},
            },
            self.hooks,
        )

        self.assertTrue(handled)
        self.assertEqual(
            self.hooks.calls,
            [
                (
                    "on_post_tool",
                    (),
                    {
                        "session_id": "s1",
                        "tool_name": "Bash",
                        "tool_input": "{'command': 'ls'}",
                        "tool_result": "{'stdout': 'README.md'}",
                    },
                )
            ],
        )

    def test_unknown_event_is_ignored(self) -> None:
        handled = dispatch({"hook_event_name": "PreModelRequest", "session_id": "s1"}, self.hooks)

        self.assertFalse(handled)
        self.assertEqual(self.hooks.calls, [])

    def test_event_without_name_is_ignored(self) -> None:
        self.assertFalse(dispatch({"session_id": "s1"}, self.hooks))
        self.assertEqual(self.hooks.calls, [])


if __name__ == "__main__":
    unittest.main()
