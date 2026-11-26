"""Tests for the SDK sink: start-time pairing and span emission.

``SdkSink`` is exercised against a fake client, so these need neither the
AgentGuard SDK nor a collector.
"""

import os
import sys
import tempfile
import time
import unittest

PLUGIN_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, PLUGIN_ROOT)

from agentguard_codex.sink import STALE_AFTER_SECONDS, SdkSink, StartStore  # noqa: E402


class FakeSpan:
    def __init__(self) -> None:
        self.attributes: dict = {}
        self.status = None
        self.ended = False

    def set_attribute(self, key, value) -> None:
        self.attributes[key] = value

    def set_status(self, status) -> None:
        self.status = status

    def end(self) -> None:
        self.ended = True


class FakeClient:
    def __init__(self) -> None:
        self.spans: list[tuple[str, dict, FakeSpan]] = []
        self.flushed = 0

    def start_span(self, name, **kwargs):
        span = FakeSpan()
        self.spans.append((name, kwargs, span))
        return span

    def flush(self) -> None:
        self.flushed += 1


class FakeHooks:
    def __init__(self) -> None:
        self.calls: list[tuple] = []

    def on_session_start(self, session_id) -> None:
        self.calls.append(("on_session_start", session_id))


class StartStoreTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.folder = os.path.join(self.tmp.name, "codex-starts")
        self.store = StartStore(self.folder)

    def test_take_returns_the_marked_time_once(self) -> None:
        before = time.time_ns()
        self.store.mark("k")
        value = self.store.take("k")

        self.assertIsNotNone(value)
        self.assertGreaterEqual(value, before)
        self.assertLessEqual(value, time.time_ns())
        self.assertIsNone(self.store.take("k"))
        self.assertEqual(os.listdir(self.folder), [])

    def test_take_without_mark(self) -> None:
        self.assertIsNone(self.store.take("never-marked"))

    def test_keys_do_not_collide(self) -> None:
        self.store.mark("a")
        self.store.mark("b")

        self.assertIsNotNone(self.store.take("a"))
        self.assertIsNotNone(self.store.take("b"))

    def test_survives_a_new_store_object(self) -> None:
        # Pre and Post hooks are separate processes sharing only the folder.
        self.store.mark("k")

        self.assertIsNotNone(StartStore(self.folder).take("k"))

    def test_corrupt_entry_is_ignored(self) -> None:
        self.store.mark("k")
        (name,) = os.listdir(self.folder)
        with open(os.path.join(self.folder, name), "w", encoding="utf-8") as f:
            f.write("not a number")

        self.assertIsNone(self.store.take("k"))

    def test_stale_entries_are_pruned(self) -> None:
        self.store.mark("abandoned")
        (name,) = os.listdir(self.folder)
        old = time.time() - STALE_AFTER_SECONDS - 60
        os.utime(os.path.join(self.folder, name), (old, old))

        self.store.mark("fresh")

        self.assertIsNone(self.store.take("abandoned"))
        self.assertIsNotNone(self.store.take("fresh"))

    def test_unwritable_folder_does_not_raise(self) -> None:
        blocker = os.path.join(self.tmp.name, "file")
        with open(blocker, "w", encoding="utf-8") as f:
            f.write("x")
        store = StartStore(os.path.join(blocker, "codex-starts"))

        store.mark("k")
        self.assertIsNone(store.take("k"))


class SdkSinkTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.hooks = FakeHooks()
        self.client = FakeClient()
        self.sink = SdkSink(self.hooks, self.client, StartStore(self.tmp.name))

    def test_lifecycle_hooks_are_delegated_to_the_sdk(self) -> None:
        self.sink.on_session_start("s1")

        self.assertEqual(self.hooks.calls, [("on_session_start", "s1")])

    def test_emit_span_sets_identifiers_and_attributes(self) -> None:
        self.sink.emit_span(
            "stop",
            session_id="s1",
            run_id="t1",
            task_id=None,
            attributes={"agentguard.hook_name": "stop", "codex.stop_hook_active": False},
        )

        ((name, kwargs, span),) = self.client.spans
        self.assertEqual(name, "stop")
        self.assertEqual(kwargs, {"session_id": "s1", "run_id": "t1", "task_id": None})
        self.assertEqual(
            span.attributes, {"agentguard.hook_name": "stop", "codex.stop_hook_active": False}
        )
        self.assertIsNone(span.status)
        self.assertTrue(span.ended)

    def test_emit_span_passes_the_start_time(self) -> None:
        self.sink.emit_span("post_compact", session_id="s1", start_time_ns=42)

        ((_, kwargs, _),) = self.client.spans
        self.assertEqual(kwargs["start_time"], 42)

    def test_mark_and_take_start(self) -> None:
        self.sink.mark_start("k")

        self.assertIsNotNone(self.sink.take_start("k"))
        self.assertIsNone(self.sink.take_start("k"))

    def test_flush(self) -> None:
        self.sink.flush()

        self.assertEqual(self.client.flushed, 1)


if __name__ == "__main__":
    unittest.main()
