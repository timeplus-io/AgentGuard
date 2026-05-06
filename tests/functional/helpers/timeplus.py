# tests/functional/helpers/timeplus.py
from __future__ import annotations

import base64
import json
import time
import urllib.error
import urllib.request


def _auth_header(user: str, password: str) -> str:
    creds = base64.b64encode(f"{user}:{password}".encode()).decode()
    return f"Basic {creds}"


def query(url: str, user: str, password: str, sql: str) -> list[dict]:
    """Run a historical SQL query against Timeplus. Returns rows as list of dicts."""
    payload = (sql.strip().rstrip(";") + " FORMAT JSON").encode()
    req = urllib.request.Request(
        f"{url}/",
        data=payload,
        headers={
            "Content-Type": "text/plain",
            "Authorization": _auth_header(user, password),
        },
    )
    with urllib.request.urlopen(req, timeout=30) as resp:
        result = json.loads(resp.read())
    return result.get("data", [])


def ingest(
    url: str,
    user: str,
    password: str,
    stream: str,
    columns: list[str],
    data: list[list],
) -> None:
    """Insert rows into a Timeplus stream via the HTTP ingest API."""
    import requests as _requests
    body = json.dumps({"columns": columns, "data": data})
    resp = _requests.post(
        f"{url}/proton/v1/ingest/streams/{stream}",
        data=body,
        headers={
            "Content-Type": "application/json",
            "Authorization": _auth_header(user, password),
        },
        timeout=30,
    )
    resp.raise_for_status()


def poll_until(
    url: str,
    user: str,
    password: str,
    sql: str,
    predicate,
    timeout_s: int = 15,
    interval_s: float = 1.0,
) -> list[dict]:
    """Poll a Timeplus query until predicate(rows) is True or timeout is reached.

    Returns the matching rows. Raises TimeoutError with last result on failure.
    """
    deadline = time.monotonic() + timeout_s
    last_rows: list[dict] = []
    while time.monotonic() < deadline:
        last_rows = query(url, user, password, sql)
        if predicate(last_rows):
            return last_rows
        time.sleep(interval_s)
    raise TimeoutError(
        f"Condition not met after {timeout_s}s. Last result: {last_rows}"
    )
