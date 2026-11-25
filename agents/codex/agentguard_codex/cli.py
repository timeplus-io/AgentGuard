"""``agentguard-codex`` — Codex hook entry point for AgentGuard.

Codex runs this command once per hook event. It reads the event JSON from
stdin and hands it to :func:`agentguard_codex.events.dispatch`, which drives
the AgentGuard Python SDK. See ``hooks.json`` for a sample Codex hook
configuration.
"""

from __future__ import annotations

import argparse
import json
import sys

from agentguard.hook import AgentGuardHooks

from agentguard_codexntguard_codex.events import dispatch


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


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)

    hooks = AgentGuardHooks(
        agent_id=args.agent_id,
        deployment_id=args.deployment_id,
        deployment_name=args.deployment_name,
        endpoint=args.endpoint,
    )

    try:
        event = json.loads(sys.stdin.read())
        dispatch(event, hooks)
    except Exception as exc:
        # Never let a plugin failure take down (or change) the Codex turn.
        print(f"agentguard-codex: {exc}", file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
