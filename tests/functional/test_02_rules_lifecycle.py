# tests/functional/test_rules_lifecycle.py
from __future__ import annotations

import os

import pytest

from helpers.api import AgentGuardClient

TIMEPLUS_USER = os.environ.get("TIMEPLUS_USER", "proton")
TIMEPLUS_PASSWORD = os.environ.get("TIMEPLUS_PASSWORD", "timeplus@t+")

# Populated by test_install_all_rules; consumed by the toggle tests below.
INSTALLED_IDS: list[str] = []


@pytest.fixture(scope="module")
def client(agentguard_url: str, auth_state: dict) -> AgentGuardClient:
    c = AgentGuardClient(agentguard_url)
    if auth_state.get("cookies"):
        c.session.cookies.update(auth_state["cookies"])
    return c


def test_install_all_rules(client: AgentGuardClient) -> None:
    """Install every rule in the catalog. Verifies each MV is created without error."""
    catalog = client.get_catalog()
    assert catalog, "Catalog returned empty — something is wrong with the server"

    failures: list[str] = []
    for item in catalog:
        rule_id = item["id"]
        try:
            result = client.install_rule(rule_id)
            assert result.get("installed") is True, (
                f"{rule_id}: expected installed=true, got {result}"
            )
            INSTALLED_IDS.append(rule_id)
        except Exception as exc:
            failures.append(f"{rule_id}: {exc}")

    assert not failures, (
        f"{len(failures)} rule(s) failed to install:\n" + "\n".join(failures)
    )


def test_toggle_disable_all_rules(client: AgentGuardClient) -> None:
    """Toggle every installed rule off. Verifies each MV can be paused."""
    assert INSTALLED_IDS, "No installed rules to toggle — test_install_all_rules must run first"

    failures: list[str] = []
    for rule_id in INSTALLED_IDS:
        try:
            result = client.toggle_rule(rule_id)
            assert result.get("enabled") is False, (
                f"{rule_id}: expected enabled=false after toggle, got {result}"
            )
        except Exception as exc:
            failures.append(f"{rule_id}: {exc}")

    assert not failures, (
        f"{len(failures)} rule(s) failed to disable:\n" + "\n".join(failures)
    )


def test_toggle_enable_all_rules(client: AgentGuardClient) -> None:
    """Toggle every installed rule back on. Verifies each MV can be resumed."""
    assert INSTALLED_IDS, "No installed rules to toggle — test_install_all_rules must run first"

    failures: list[str] = []
    for rule_id in INSTALLED_IDS:
        try:
            result = client.toggle_rule(rule_id)
            assert result.get("enabled") is True, (
                f"{rule_id}: expected enabled=true after re-toggle, got {result}"
            )
        except Exception as exc:
            failures.append(f"{rule_id}: {exc}")

    assert not failures, (
        f"{len(failures)} rule(s) failed to re-enable:\n" + "\n".join(failures)
    )
