"""Unit tests for ingest URL / auth-header construction (#299).

Both entry points (the packaged ``agentguard_hermes.plugin`` and the
gateway-hook ``handler.py``) read their config from AGENTGUARD_* env vars at
import time, so each case sets the env and reloads the module.

Run:  python3.12 -m unittest discover -s tests -v
"""
import importlib
import os
import sys
import unittest
from base64 import b64encode

PLUGIN_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, PLUGIN_ROOT)

MODULES = ["agentguard_hermes.plugin", "handler"]

ENV_KEYS = [
    "AGENTGUARD_TIMEPLUS_URL", "AGENTGUARD_DATABASE", "AGENTGUARD_STREAM",
    "AGENTGUARD_USERNAME", "AGENTGUARD_PASSWORD",
    "AGENTGUARD_INGEST_API", "AGENTGUARD_WORKSPACE",
]


def _load(name: str, env: dict):
    for k in ENV_KEYS:
        os.environ.pop(k, None)
    os.environ.update(env)
    mod = importlib.import_module(name)
    return importlib.reload(mod)


class IngestUrlTests(unittest.TestCase):
    def tearDown(self):
        for k in ENV_KEYS:
            os.environ.pop(k, None)

    def test_default_is_timeplusd_legacy_path_with_basic_auth(self):
        for name in MODULES:
            with self.subTest(module=name):
                m = _load(name, {"AGENTGUARD_USERNAME": "proton", "AGENTGUARD_PASSWORD": "pw"})
                self.assertEqual(m._INGEST_API, "timeplusd")
                self.assertEqual(
                    m._ingest_url(),
                    "http://localhost:3218/timeplusd/v2/ingest/ag/streams/agentguard_hook_events",
                )
                token = b64encode(b"proton:pw").decode()
                self.assertEqual(m._auth_headers(), {"Authorization": f"Basic {token}"})

    def test_rest_mode_builds_workspace_prefixed_v1beta2_url(self):
        for name in MODULES:
            with self.subTest(module=name):
                m = _load(name, {
                    "AGENTGUARD_INGEST_API": "rest",
                    "AGENTGUARD_TIMEPLUS_URL": "http://timeplus.example.com:8000/",
                })
                self.assertEqual(
                    m._ingest_url(),
                    "http://timeplus.example.com:8000/default/api/v1beta2/streams/agentguard_hook_events/ingest?db=ag",
                )

    def test_rest_mode_uses_configured_workspace_and_database(self):
        for name in MODULES:
            with self.subTest(module=name):
                m = _load(name, {
                    "AGENTGUARD_INGEST_API": "rest",
                    "AGENTGUARD_TIMEPLUS_URL": "http://localhost:8000",
                    "AGENTGUARD_WORKSPACE": "ws-42",
                    "AGENTGUARD_DATABASE": "other",
                })
                self.assertEqual(
                    m._ingest_url(),
                    "http://localhost:8000/ws-42/api/v1beta2/streams/agentguard_hook_events/ingest?db=other",
                )

    def test_rest_mode_uses_basic_auth_with_username_password(self):
        for name in MODULES:
            with self.subTest(module=name):
                m = _load(name, {
                    "AGENTGUARD_INGEST_API": "rest",
                    "AGENTGUARD_USERNAME": "u",
                    "AGENTGUARD_PASSWORD": "p",
                })
                token = b64encode(b"u:p").decode()
                self.assertEqual(m._auth_headers(), {"Authorization": f"Basic {token}"})

    def test_ingest_api_is_case_and_whitespace_insensitive(self):
        for name in MODULES:
            with self.subTest(module=name):
                m = _load(name, {"AGENTGUARD_INGEST_API": " REST "})
                self.assertEqual(m._INGEST_API, "rest")

    def test_unknown_ingest_api_falls_back_to_timeplusd(self):
        for name in MODULES:
            with self.subTest(module=name):
                m = _load(name, {"AGENTGUARD_INGEST_API": "bogus"})
                self.assertEqual(m._INGEST_API, "timeplusd")


if __name__ == "__main__":
    unittest.main()
