"""``agentguard-codex`` — Codex hook entry point for AgentGuard.

Codex runs this command once per hook event. It reads the event JSON from
stdin and hands it to :func:`agentguard_codex.events.dispatch`, which drives
the AgentGuard Python SDK. See ``hooks.json`` for a sample Codex hook
configuration.
"""

from __future__ import annotations

import argparse
import json
import os
import socket
import sys

from agentguard_codex.events import dispatch

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
    """Resolve the agent id the SDK also uses to key its on-disk store.

    The SDK's ``AgentGuardHooks`` falls back to the literal folder name
    ``hostname`` when ``agent_id`` is ``None``, so resolve the env-var fallback
    here and pass the result through; otherwise agents that omit ``--agent-id``
    would all share one Pre/Post-tool timing store.
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


def main(argv: list[str] | None = None) -> int:
    try:
        args = build_parser().parse_args(argv)

        # Imported lazily so ``import agentguard_codex.cli`` (and ``--help``)
        # works without the AgentGuard SDK, and a missing SDK is handled by
        # the same fail-safe path as any other plugin error.
        from agentguard.hook import AgentGuardHooks

        hooks = AgentGuardHooks(
            agent_id=resolve_agent_id(args.agent_id),
            deployment_id=resolve_deployment_id(args.deployment_id),
            deployment_name=resolve_deployment_name(args.deployment_name),
            endpoint=resolve_endpoint(args.endpoint),
        )

        event = json.loads(sys.stdin.read())
        dispatch(event, hooks)
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
