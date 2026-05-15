# tests/functional/helpers/api.py
from __future__ import annotations

import json
from typing import Iterator

import requests


class AgentGuardClient:
    """Thin wrapper around requests.Session for the AgentGuard REST API."""

    def __init__(self, base_url: str) -> None:
        self.base_url = base_url.rstrip("/")
        self.session = requests.Session()

    def test_connection(
        self, host: str, port: int, username: str, password: str
    ) -> dict:
        """POST /api/setup/test-connection. Returns {"ok": true} or raises."""
        resp = self.session.post(
            f"{self.base_url}/api/setup/test-connection",
            json={"host": host, "port": port, "username": username, "password": password},
            timeout=15,
        )
        resp.raise_for_status()
        return resp.json()

    def provision(
        self,
        host: str,
        port: int,
        native_port: int,
        username: str,
        password: str,
        agentguard_url: str,
    ) -> Iterator[dict]:
        """POST /api/setup/provision — yields parsed SSE event dicts until stream ends."""
        resp = self.session.post(
            f"{self.base_url}/api/setup/provision",
            json={
                "host": host,
                "port": port,
                "native_port": native_port,
                "username": username,
                "password": password,
                "agentguard_url": agentguard_url,
            },
            stream=True,
            timeout=180,
        )
        resp.raise_for_status()
        for raw_line in resp.iter_lines():
            if raw_line and raw_line.startswith(b"data: "):
                yield json.loads(raw_line[6:])

    def login(self, username: str, password: str) -> requests.Response:
        """POST /api/auth/login. Stores session cookie on success."""
        resp = self.session.post(
            f"{self.base_url}/api/auth/login",
            json={"username": username, "password": password},
            timeout=15,
        )
        resp.raise_for_status()
        return resp

    def get_catalog(self) -> list[dict]:
        """GET /api/rules/catalog. Returns list of catalog items."""
        resp = self.session.get(f"{self.base_url}/api/rules/catalog", timeout=30)
        resp.raise_for_status()
        return resp.json().get("catalog", [])

    def install_rule(self, rule_id: str) -> dict:
        """POST /api/rules/{id}/install. Returns {"installed": true, "rule_id": id}."""
        resp = self.session.post(
            f"{self.base_url}/api/rules/{rule_id}/install", timeout=30
        )
        if not resp.ok:
            raise RuntimeError(f"HTTP {resp.status_code}: {resp.text[:800]}")
        return resp.json()

    def toggle_rule(self, rule_id: str) -> dict:
        """POST /api/rules/{id}/toggle. Returns RulePack with updated enabled field."""
        resp = self.session.post(
            f"{self.base_url}/api/rules/{rule_id}/toggle", timeout=30
        )
        if not resp.ok:
            raise RuntimeError(f"HTTP {resp.status_code}: {resp.text[:800]}")
        return resp.json()

    def get_rule(self, rule_id: str) -> dict:
        """GET /api/rules/{id}. Returns RuleDetailResponse."""
        resp = self.session.get(
            f"{self.base_url}/api/rules/{rule_id}", timeout=15
        )
        resp.raise_for_status()
        return resp.json()

    def list_threats(self, agent_id: str | None = None) -> list[dict]:
        """GET /api/threats. Returns list from {"threats": [...]} response."""
        params: dict = {}
        if agent_id:
            params["agent_id"] = agent_id
        resp = self.session.get(
            f"{self.base_url}/api/threats", params=params, timeout=15
        )
        resp.raise_for_status()
        return resp.json().get("threats", [])
