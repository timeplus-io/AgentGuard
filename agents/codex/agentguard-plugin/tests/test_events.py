"""Unit tests for Codex hook event translation.

The tests use a recording sink instead of the real AgentGuard SDK, so they
need no dependencies and no running collector.

Run:  python3.12 -m unittest discover -s tests -v
"""

import json
import os
import sys
import unittest

PLUGIN_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, PLUGIN_ROOT)

from agentguard_codex.events import (  # noqa: E402
    HOOK_SPAN_NAMES,
    SUPPORTED_EVENTS,
    dispatch,
)


class RecordingHooks:
    """Records every SDK call made by :func:`dispatch`."""

    def __init__(self, starts: dict[str, int] | None = None) -> None:
        self.calls: list[tuple[str, tuple, dict]] = []
        self.starts = dict(starts or {})
        self.marked: list[str] = []

    def mark_start(self, key: str) -> None:
        self.marked.append(key)
        self.starts[key] = 1_000

    def take_start(self, key: str) -> int | None:
        return self.starts.pop(key, None)

    def __getattr__(self, name: str):
        def record(*args, **kwargs):
            self.calls.append((name, args, kwargs))

        return record


# Fields Codex sends with every turn-scoped hook.
COMMON = {
    "session_id": "s1",
    "turn_id": "t1",
    "transcript_path": "/tmp/rollout.jsonl",
    "cwd": "/repo",
    "model": "gpt-5-codex",
}


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

    def test_user_prompt_submit_without_optional_fields(self) -> None:
        handled = dispatch(
            {"hook_event_name": "UserPromptSubmit", "session_id": "s1"}, self.hooks
        )

        self.assertTrue(handled)
        self.assertEqual(
            self.hooks.calls,
            [
                (
                    "on_llm_end",
                    (),
                    {"session_id": "s1", "task_id": None, "model_name": None, "user_message": None},
                )
            ],
        )

    def test_pre_tool_use(self) -> None:
        handled = dispatch(
            {
                "hook_event_name": "PreToolUse",
                "session_id": "s1",
                "turn_id": "t1",
                "tool_use_id": "call_1",
                "tool_name": "Bash",
            },
            self.hooks,
        )

        self.assertTrue(handled)
        self.assertEqual(
            self.hooks.calls,
            [("on_pre_tool", (), {"session_id": "s1", "run_id": "t1", "task_id": "call_1"})],
        )

    def test_post_tool_use_json_encodes_inputs(self) -> None:
        handled = dispatch(
            {
                "hook_event_name": "PostToolUse",
                "session_id": "s1",
                "turn_id": "t1",
                "tool_use_id": "call_1",
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
                        "run_id": "t1",
                        "task_id": "call_1",
                        "tool_name": "Bash",
                        "tool_input": '{"command": "ls"}',
                        "tool_result": '{"stdout": "README.md"}',
                    },
                )
            ],
        )

    def test_post_tool_use_passes_strings_through(self) -> None:
        handled = dispatch(
            {
                "hook_event_name": "PostToolUse",
                "session_id": "s1",
                "tool_name": "Bash",
                "tool_input": "ls -la",
                "tool_response": "total 0",
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
                        "run_id": None,
                        "task_id": None,
                        "tool_name": "Bash",
                        "tool_input": "ls -la",
                        "tool_result": "total 0",
                    },
                )
            ],
        )

    def test_unknown_event_is_ignored(self) -> None:
        handled = dispatch({"hook_event_name": "SomeFutureHook", "session_id": "s1"}, self.hooks)

        self.assertFalse(handled)
        self.assertEqual(self.hooks.calls, [])

    def test_event_without_name_is_ignored(self) -> None:
        self.assertFalse(dispatch({"session_id": "s1"}, self.hooks))
        self.assertEqual(self.hooks.calls, [])


class SpanEventTests(unittest.TestCase):
    """The nine events emitted as their own span, with representative payloads."""

    def setUp(self) -> None:
        self.hooks = RecordingHooks()

    def emit(self, event: dict, hooks: RecordingHooks | None = None) -> dict:
        """Dispatch *event* and return the single ``emit_span`` call's kwargs."""
        hooks = hooks or self.hooks
        self.assertTrue(dispatch(event, hooks))
        self.assertEqual([name for name, _, _ in hooks.calls], ["emit_span"])
        _, args, kwargs = hooks.calls[0]
        return {"name": args[0], **kwargs}

    def test_pre_model_request(self) -> None:
        span = self.emit(
            {
                **COMMON,
                "hook_event_name": "PreModelRequest",
                "request_id": "req_1",
                "attempt_number": 1,
                "kind": "sampling",
                "thread_id": "thr_1",
            }
        )

        self.assertEqual(span["name"], "pre_model_request")
        self.assertEqual(span["session_id"], "s1")
        self.assertEqual(span["run_id"], "t1")
        self.assertEqual(span["task_id"], "req_1")
        self.assertIsNone(span["start_time_ns"])
        self.assertIsNone(span["error"])
        self.assertEqual(
            span["attributes"],
            {
                "agentguard.hook_name": "pre_model_request",
                "llm.model_name": "gpt-5-codex",
                "codex.request.id": "req_1",
                "codex.request.attempt": 1,
                "codex.request.kind": "sampling",
                "codex.thread_id": "thr_1",
            },
        )
        self.assertEqual(len(self.hooks.marked), 1)

    def test_post_model_request_completed_with_usage(self) -> None:
        pre = {
            **COMMON,
            "hook_event_name": "PreModelRequest",
            "request_id": "req_1",
            "attempt_number": 1,
        }
        dispatch(pre, self.hooks)
        self.hooks.calls.clear()

        span = self.emit(
            {
                **pre,
                "hook_event_name": "PostModelRequest",
                "status": "completed",
                "error": None,
                "token_usage": {
                    "input_tokens": 1200,
                    "cached_input_tokens": 800,
                    "cache_write_input_tokens": 50,
                    "output_tokens": 300,
                    "reasoning_output_tokens": 120,
                    "total_tokens": 1500,
                },
            }
        )

        self.assertEqual(span["name"], "post_model_request")
        self.assertEqual(span["start_time_ns"], 1_000)  # paired with the Pre event
        self.assertIsNone(span["error"])
        attributes = span["attributes"]
        self.assertEqual(attributes["codex.request.status"], "completed")
        self.assertNotIn("codex.request.error", attributes)
        self.assertEqual(attributes["llm.token_count.prompt"], 1200)
        self.assertEqual(attributes["llm.token_count.completion"], 300)
        self.assertEqual(attributes["llm.token_count.total"], 1500)
        self.assertEqual(attributes["llm.token_count.prompt_details.cache_read"], 800)
        self.assertEqual(attributes["llm.token_count.prompt_details.cache_write"], 50)
        self.assertEqual(attributes["llm.token_count.completion_details.reasoning"], 120)
        # Usage lives on the attempt, never on a span a consumer counts as an LLM call.
        self.assertNotIn("openinference.span.kind", attributes)

    def test_post_model_request_failed_without_usage(self) -> None:
        span = self.emit(
            {
                **COMMON,
                "hook_event_name": "PostModelRequest",
                "request_id": "req_1",
                "attempt_number": 2,
                "status": "failed",
                "error": "stream disconnected",
                "token_usage": None,
            }
        )

        self.assertEqual(span["error"], "stream disconnected")
        self.assertIsNone(span["start_time_ns"])  # no matching Pre event seen
        self.assertEqual(span["attributes"]["codex.request.status"], "failed")
        self.assertEqual(span["attributes"]["codex.request.error"], "stream disconnected")
        self.assertEqual(span["attributes"]["codex.request.attempt"], 2)
        self.assertFalse([k for k in span["attributes"] if k.startswith("llm.token_count")])

    def test_post_model_request_cancelled_is_not_an_error(self) -> None:
        span = self.emit(
            {**COMMON, "hook_event_name": "PostModelRequest", "status": "cancelled"}
        )

        self.assertIsNone(span["error"])
        self.assertEqual(span["attributes"]["codex.request.status"], "cancelled")

    def test_retries_pair_with_their_own_attempt(self) -> None:
        base = {**COMMON, "request_id": "req_1"}
        for attempt in (1, 2):
            dispatch(
                {**base, "hook_event_name": "PreModelRequest", "attempt_number": attempt},
                self.hooks,
            )
        first, second = self.hooks.marked
        self.assertNotEqual(first, second)

        self.hooks.starts[first] = 111
        self.hooks.starts[second] = 222
        self.hooks.calls.clear()
        span = self.emit(
            {**base, "hook_event_name": "PostModelRequest", "attempt_number": 2}
        )
        self.assertEqual(span["start_time_ns"], 222)

    def test_stop(self) -> None:
        span = self.emit(
            {
                **COMMON,
                "hook_event_name": "Stop",
                "permission_mode": "default",
                "stop_hook_active": False,
                "last_assistant_message": "Done. All tests pass.",
            }
        )

        self.assertEqual(span["name"], "stop")
        self.assertEqual(span["run_id"], "t1")
        self.assertEqual(
            span["attributes"],
            {
                "agentguard.hook_name": "stop",
                "llm.model_name": "gpt-5-codex",
                "codex.permission_mode": "default",
                "output.value": "Done. All tests pass.",
                "codex.stop_hook_active": False,
            },
        )

    def test_stop_without_assistant_message(self) -> None:
        span = self.emit(
            {**COMMON, "hook_event_name": "Stop", "last_assistant_message": None}
        )

        self.assertNotIn("output.value", span["attributes"])

    def test_stop_does_not_end_the_session(self) -> None:
        dispatch({**COMMON, "hook_event_name": "Stop"}, self.hooks)

        self.assertNotIn("on_session_end", [name for name, _, _ in self.hooks.calls])

    def test_interrupt(self) -> None:
        span = self.emit(
            {**COMMON, "hook_event_name": "Interrupt", "permission_mode": "acceptEdits"}
        )

        self.assertEqual(span["name"], "interrupt")
        self.assertEqual(span["run_id"], "t1")
        self.assertEqual(
            span["attributes"],
            {
                "agentguard.hook_name": "interrupt",
                "llm.model_name": "gpt-5-codex",
                "codex.permission_mode": "acceptEdits",
            },
        )

    def test_subagent_start(self) -> None:
        span = self.emit(
            {
                **COMMON,
                "hook_event_name": "SubagentStart",
                "agent_id": "agent_7",
                "agent_type": "explorer",
                "permission_mode": "default",
            }
        )

        self.assertEqual(span["name"], "subagent_start")
        self.assertEqual(span["session_id"], "s1")  # the parent session
        self.assertEqual(span["task_id"], "agent_7")
        self.assertEqual(span["attributes"]["agentguard.subagent.id"], "agent_7")
        self.assertEqual(span["attributes"]["agentguard.subagent.type"], "explorer")

    def test_subagent_stop(self) -> None:
        start = {
            **COMMON,
            "hook_event_name": "SubagentStart",
            "agent_id": "agent_7",
            "agent_type": "explorer",
        }
        dispatch(start, self.hooks)
        self.hooks.calls.clear()

        span = self.emit(
            {
                **start,
                "hook_event_name": "SubagentStop",
                "agent_transcript_path": "/tmp/agent_7.jsonl",
                "stop_hook_active": True,
                "last_assistant_message": "Found 3 call sites.",
            }
        )

        self.assertEqual(span["name"], "subagent_stop")
        self.assertEqual(span["start_time_ns"], 1_000)
        self.assertEqual(span["task_id"], "agent_7")
        attributes = span["attributes"]
        self.assertEqual(attributes["agentguard.subagent.id"], "agent_7")
        self.assertEqual(attributes["agentguard.subagent.type"], "explorer")
        self.assertEqual(attributes["output.value"], "Found 3 call sites.")
        self.assertEqual(attributes["codex.agent_transcript_path"], "/tmp/agent_7.jsonl")
        self.assertIs(attributes["codex.stop_hook_active"], True)

    def test_subagent_stop_with_null_optionals(self) -> None:
        span = self.emit(
            {
                **COMMON,
                "hook_event_name": "SubagentStop",
                "agent_id": "agent_7",
                "agent_type": "explorer",
                "agent_transcript_path": None,
                "last_assistant_message": None,
            }
        )

        self.assertNotIn("output.value", span["attributes"])
        self.assertNotIn("codex.agent_transcript_path", span["attributes"])

    def test_permission_request(self) -> None:
        span = self.emit(
            {
                **COMMON,
                "hook_event_name": "PermissionRequest",
                "permission_mode": "default",
                "tool_name": "Bash",
                "tool_input": {"command": "rm -rf build", "description": "Remove build output"},
            }
        )

        self.assertEqual(span["name"], "permission_request")
        attributes = span["attributes"]
        self.assertEqual(attributes["tool.name"], "Bash")
        self.assertEqual(
            json.loads(attributes["tool.parameters"]),
            {"command": "rm -rf build", "description": "Remove build output"},
        )
        self.assertEqual(attributes["codex.permission.description"], "Remove build output")
        # Observed only: nothing in the span records or implies a decision.
        self.assertFalse([k for k in attributes if "decision" in k])

    def test_permission_request_without_description(self) -> None:
        span = self.emit(
            {
                **COMMON,
                "hook_event_name": "PermissionRequest",
                "tool_name": "mcp__fs__write",
                "tool_input": "raw string input",
            }
        )

        self.assertEqual(span["attributes"]["tool.parameters"], "raw string input")
        self.assertNotIn("codex.permission.description", span["attributes"])

    def test_pre_and_post_compact(self) -> None:
        for trigger in ("manual", "auto"):
            with self.subTest(trigger=trigger):
                hooks = RecordingHooks()
                event = {**COMMON, "hook_event_name": "PreCompact", "trigger": trigger}
                pre = self.emit(event, hooks)
                self.assertEqual(pre["name"], "pre_compact")
                self.assertEqual(pre["attributes"]["codex.compact.trigger"], trigger)
                self.assertIsNone(pre["start_time_ns"])

                hooks.calls.clear()
                post = self.emit({**event, "hook_event_name": "PostCompact"}, hooks)
                self.assertEqual(post["name"], "post_compact")
                self.assertEqual(post["attributes"]["codex.compact.trigger"], trigger)
                self.assertEqual(post["start_time_ns"], 1_000)

    def test_minimal_payload_for_every_span_event(self) -> None:
        # Only the two universal fields: every optional attribute is left off.
        for event_name, span_name in HOOK_SPAN_NAMES.items():
            with self.subTest(event=event_name):
                hooks = RecordingHooks()
                span = self.emit({"hook_event_name": event_name, "session_id": "s1"}, hooks)
                self.assertEqual(span["name"], span_name)
                self.assertIsNone(span["run_id"])
                self.assertEqual(span["attributes"], {"agentguard.hook_name": span_name})

    def test_span_names_are_distinct(self) -> None:
        names = list(HOOK_SPAN_NAMES.values())
        self.assertEqual(len(names), len(set(names)))
        # ...and none collides with the SDK's own session / llm / tool spans.
        self.assertFalse(set(names) & {"session", "llm", "tool"})


class CoverageTests(unittest.TestCase):
    """The shipped sample, the handlers and the hooks reference must agree."""

    CODEX_HOOKS = {
        "SessionStart",
        "SessionEnd",
        "UserPromptSubmit",
        "PreModelRequest",
        "PostModelRequest",
        "PreToolUse",
        "PermissionRequest",
        "PostToolUse",
        "PreCompact",
        "PostCompact",
        "SubagentStart",
        "SubagentStop",
        "Stop",
        "Interrupt",
    }

    def setUp(self) -> None:
        with open(os.path.join(PLUGIN_ROOT, "hooks.json"), encoding="utf-8") as f:
            self.sample = json.load(f)["hooks"]

    def test_sample_registers_exactly_the_supported_events(self) -> None:
        self.assertEqual(set(self.sample), set(SUPPORTED_EVENTS))

    def test_every_codex_hook_is_supported(self) -> None:
        self.assertEqual(set(SUPPORTED_EVENTS), self.CODEX_HOOKS)

    def test_every_registered_event_is_handled(self) -> None:
        for event_name in self.sample:
            with self.subTest(event=event_name):
                hooks = RecordingHooks()
                handled = dispatch({"hook_event_name": event_name, "session_id": "s1"}, hooks)
                self.assertTrue(handled)
                self.assertTrue(hooks.calls)

    def test_sample_entries_run_the_plugin_command(self) -> None:
        for event_name, groups in self.sample.items():
            for group in groups:
                for hook in group["hooks"]:
                    with self.subTest(event=event_name):
                        self.assertEqual(hook["type"], "command")
                        self.assertEqual(hook["command"], "agentguard-codex")

    def test_short_budget_hooks_stay_within_the_codex_limit(self) -> None:
        # Codex caps SessionEnd and Interrupt hook timeouts at 3 seconds.
        for event_name in ("SessionEnd", "Interrupt"):
            for group in self.sample[event_name]:
                for hook in group["hooks"]:
                    self.assertLessEqual(hook["timeout"], 3)


if __name__ == "__main__":
    unittest.main()
