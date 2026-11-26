"""``agentguard-codex`` — Codex hook entry point for AgentGuard.

Codex runs this command once per hook event. It reads the event JSON from
stdin and hands it to :func:`agentguard_codex.events.dispatch`, which emits
spans through the AgentGuard Python SDK. See ``hooks.json`` for a sample Codex hook
configuration.
"""

from __future__ import annotations

import argparse
import json
import os
import socket
import sys

from agentguard_codex.events import dispatch

AGENT_TYPE = "codex"
DEFAULT_DEPLOYMENT_ID = "local"
DEFAULT_DEPLOYMENT_NAME = "Local Dev"
DEFAULT_ENDPOINT = "http://localhost:4318"


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="agentguard-codex",
        description="Forward OpenAI Codex hook events to AgentGuard",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=(
            "Reads one Codex hook event as JSON on stdin and exports\n"
            "OpenTelemetry spans to the AgentGuard Timeplus app's OTLP input."
        ),
    )
    parser.add_argument(
        "--agent-id",
        default=None,
        help="Unique agent identifier (default: $AGENTGUARD_AGENT_ID or hostname)",
    )
    parser.add_argument(
        "--deployment-id",
        default=None,
        help="Deployment identifier (default: $AGENTGUARD_DEPLOYMENT_ID or 'local')",
    )
    parser.add_argument(
        "--deployment-name",
        default=None,
        help="Human-readable deployment name (default: $AGENTGUARD_DEPLOYMENT_NAME or 'Local Dev')",
    )
    parser.add_argument(
        "--endpoint",
        default=None,
        help="OTLP/HTTP endpoint (default: $OTEL_EXPORTER_OTLP_ENDPOINT or http://localhost:4318)",
    )
    return parser


def resolve_agent_id(agent_id: str | None) -> str:
    """Resolve the agent id: flag, then env, then the hostname.

    Resolved here, not left to the SDK, because the same value names the
    folder of the on-disk start-time store.
    """
    return agent_id or os.environ.get("AGENTGUARD_AGENT_ID") or socket.gethostname()


def resolve_deployment_id(deployment_id: str | None) -> str:
    """Resolve the deployment id: flag, then env, then ``local``."""
    return (
        deployment_id
        or os.environ.get("AGENTGUARD_DEPLOYMENT_ID")
        or DEFAULT_DEPLOYMENT_ID
    )


def resolve_deployment_name(deployment_name: str | None) -> str:
    """Resolve the deployment name: flag, then env, then ``Local Dev``."""
    return (
        deployment_name
        or os.environ.get("AGENTGUARD_DEPLOYMENT_NAME")
        or DEFAULT_DEPLOYMENT_NAME
    )


def resolve_endpoint(endpoint: str | None) -> str:
    """Resolve the OTLP/HTTP endpoint: flag, then env, then localhost:4318."""
    return (
        endpoint
        or os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT")
        or DEFAULT_ENDPOINT
    )


def start_store_folder(agent_id: str) -> str:
    """Where the start times of open sessions, tools and requests live."""
    return os.path.join(os.path.expanduser("~"), ".agentguard", agent_id, "codex-starts")


def main(argv: list[str] | None = None) -> int:
    try:
        args = build_parser().parse_args(argv)

        # Imported lazily so ``import agentguard_codex.cli`` (and ``--help``)
        # works without the AgentGuard SDK, and a missing SDK is handled by
        # the same fail-safe path as any other plugin error.
        from agentguard import AgentGuard

        from agentguard_codex.sink import SdkSink, StartStore

        agent_id = resolve_agent_id(args.agent_id)
        client = AgentGuard(
            # The app reports the OTel service name as the agent type.
            service_name=AGENT_TYPE,
            agent_id=agent_id,
            deployment_id=resolve_deployment_id(args.deployment_id),
            deployment_name=resolve_deployment_name(args.deployment_name),
            otlp_endpoint=resolve_endpoint(args.endpoint),
        )
        sink = SdkSink(client, StartStore(start_store_folder(agent_id)))

        try:
            dispatch(json.loads(sys.stdin.read()), sink)
        finally:
            sink.close()
    except SystemExit:
        # argparse raises SystemExit for --help (0) and bad flags (2). A bad
        # flag must not change the Codex turn, so report success either way.
        return 0
    except Exception as exc:
        # Never let a plugin failure take down (or change) the Codex turn.
        print(f"agentguard-codex: {exc}", file=sys.stderr)

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
