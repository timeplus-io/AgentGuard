# tests/functional/test_onboarding.py
from __future__ import annotations

import os

import pytest

from helpers.api import AgentGuardClient

TIMEPLUS_USER = os.environ.get("TIMEPLUS_USER", "proton")
TIMEPLUS_PASSWORD = os.environ.get("TIMEPLUS_PASSWORD", "timeplus@t+")


@pytest.fixture(scope="module")
def client(agentguard_url: str) -> AgentGuardClient:
    return AgentGuardClient(agentguard_url)


def test_connection_ok(
    client: AgentGuardClient,
    timeplus_internal_host: str,
) -> None:
    """Server can reach Timeplus using internal Docker network address."""
    result = client.test_connection(
        host=timeplus_internal_host,
        port=3218,
        username=TIMEPLUS_USER,
        password=TIMEPLUS_PASSWORD,
    )
    assert result.get("ok") is True, f"test-connection failed: {result}"


def test_provision(
    client: AgentGuardClient,
    timeplus_internal_host: str,
    agentguard_url: str,
) -> None:
    """Provision creates all Timeplus streams and MVs without error."""
    events = list(
        client.provision(
            host=timeplus_internal_host,
            port=3218,
            native_port=8463,
            username=TIMEPLUS_USER,
            password=TIMEPLUS_PASSWORD,
            agentguard_url=agentguard_url,
        )
    )

    statuses = [e.get("status") for e in events]
    error_events = [e for e in events if e.get("status") == "error"]
    assert not error_events, f"Provision reported error(s): {error_events}"
    assert "done" in statuses, f"Provision never reached 'done'. Events: {events}"


def test_login(
    client: AgentGuardClient,
    auth_state: dict,
) -> None:
    """Login with Timeplus credentials succeeds and returns a session cookie."""
    resp = client.login(TIMEPLUS_USER, TIMEPLUS_PASSWORD)
    assert resp.status_code == 200, f"Login failed: {resp.text}"
    cookies = dict(client.session.cookies)
    assert cookies, "No session cookie returned from login"
    auth_state["cookies"] = cookies
