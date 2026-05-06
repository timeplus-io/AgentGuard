# tests/functional/conftest.py
from __future__ import annotations

import os
import pathlib
import socket
import time
import uuid

import docker
import docker.errors
import pytest
import requests

# ---------------------------------------------------------------------------
# Configuration — all overridable via environment variables
# ---------------------------------------------------------------------------

TIMEPLUS_IMAGE = os.environ.get(
    "TIMEPLUS_IMAGE", "timeplus/timeplus-enterprise:3.2.1"
)
AGENTGUARD_IMAGE = os.environ.get("AGENTGUARD_IMAGE", "")
TIMEPLUS_USER = os.environ.get("TIMEPLUS_USER", "proton")
TIMEPLUS_PASSWORD = os.environ.get("TIMEPLUS_PASSWORD", "timeplus@t+")

# Repo root is three levels up from tests/functional/conftest.py
REPO_ROOT = str(pathlib.Path(__file__).parent.parent.parent)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _free_port() -> int:
    """Return an available TCP port on localhost."""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("", 0))
        return s.getsockname()[1]


def _wait_http(url: str, timeout_s: int, interval_s: float = 2.0) -> None:
    """Block until url returns a non-5xx response, or fail the test."""
    deadline = time.monotonic() + timeout_s
    while time.monotonic() < deadline:
        try:
            resp = requests.get(url, timeout=3)
            if resp.status_code < 500:
                return
        except Exception:
            pass
        time.sleep(interval_s)
    pytest.fail(f"Service at {url} did not become ready within {timeout_s}s")


# ---------------------------------------------------------------------------
# Session-scoped Docker fixtures
# ---------------------------------------------------------------------------

@pytest.fixture(scope="session")
def docker_client():
    """Connect to the local Docker daemon. Fail fast if Docker is unavailable."""
    try:
        client = docker.from_env()
        client.ping()
        return client
    except Exception as exc:
        pytest.fail(f"Docker is not available: {exc}")


@pytest.fixture(scope="session")
def docker_network(docker_client):
    """Create an isolated bridge network for the test session."""
    name = f"agentguard_test_{uuid.uuid4().hex[:8]}"
    network = docker_client.networks.create(name, driver="bridge")
    yield network
    try:
        network.remove()
    except Exception:
        pass


@pytest.fixture(scope="session")
def timeplus_container(docker_client, docker_network):
    """Start the Timeplus container and wait for the HTTP port to be ready."""
    host_port = _free_port()
    container = docker_client.containers.run(
        TIMEPLUS_IMAGE,
        platform="linux/amd64",
        name=f"timeplus_{uuid.uuid4().hex[:6]}",
        network=docker_network.name,
        ports={"3218/tcp": host_port},
        detach=True,
        remove=False,
    )
    try:
        _wait_http(f"http://localhost:{host_port}/", timeout_s=90)
        yield container
    finally:
        try:
            container.stop(timeout=10)
        except Exception:
            pass
        try:
            container.remove(force=True)
        except Exception:
            pass


@pytest.fixture(scope="session")
def timeplus_url(timeplus_container) -> str:
    """External URL for host-side Timeplus queries and ingest."""
    timeplus_container.reload()
    port = timeplus_container.ports["3218/tcp"][0]["HostPort"]
    return f"http://localhost:{port}"


@pytest.fixture(scope="session")
def timeplus_internal_host(timeplus_container) -> str:
    """Container hostname for server-to-server calls (provision, internal config)."""
    return timeplus_container.name


@pytest.fixture(scope="session")
def agentguard_container(docker_client, docker_network, timeplus_container):
    """Build (or pull) the AgentGuard image and start it on the test network."""
    host_port = _free_port()
    timeplus_container.reload()
    timeplus_name = timeplus_container.name

    if AGENTGUARD_IMAGE:
        image_tag = AGENTGUARD_IMAGE
        docker_client.images.pull(AGENTGUARD_IMAGE)
    else:
        image_tag = "agentguard:test"
        docker_client.images.build(path=REPO_ROOT, tag=image_tag, rm=True)

    container_name = f"agentguard_{uuid.uuid4().hex[:6]}"
    container = docker_client.containers.run(
        image_tag,
        name=container_name,
        network=docker_network.name,
        ports={"8080/tcp": host_port},
        environment={
            "TIMEPLUS_HOST": timeplus_name,
            "TIMEPLUS_PORT": "3218",
            "TIMEPLUS_USER": TIMEPLUS_USER,
            "TIMEPLUS_PASSWORD": TIMEPLUS_PASSWORD,
            "SERVER_PORT": "8080",
            "AGENTGUARD_URL": f"http://{container_name}:8080",
        },
        detach=True,
        remove=False,
    )
    try:
        _wait_http(f"http://localhost:{host_port}/api/auth/status", timeout_s=60)
        yield container
    finally:
        try:
            container.stop(timeout=10)
        except Exception:
            pass
        try:
            container.remove(force=True)
        except Exception:
            pass


@pytest.fixture(scope="session")
def agentguard_url(agentguard_container) -> str:
    """External URL for host-side AgentGuard API calls."""
    agentguard_container.reload()
    port = agentguard_container.ports["8080/tcp"][0]["HostPort"]
    return f"http://localhost:{port}"


@pytest.fixture(scope="session")
def auth_state() -> dict:
    """Mutable dict shared across all test files for storing the session cookie.

    test_onboarding.py::test_login writes {"cookies": {...}} here.
    All later test files read it to make authenticated requests.
    """
    return {}
