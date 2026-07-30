#!/usr/bin/env python3
"""
AgentGuard CLI - Session management and OpenTelemetry trace emission

Usage:
    agentguard session_start [session_id] [run_id]
    agentguard session_end [session_id] [status]
    agentguard session_trace [trace_id] [resource_id]

Examples:
    agentguard --agent-id my-agent --deployment-id prod --deployment-name "Production" session_start mysession123 run456
    agentguard --endpoint http://localhost:4318 --deployment-id dev session_end mysession123 error
"""

from __future__ import annotations

import argparse
import sys

from agentguard.hook import AgentGuardHooks


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser(
        description="AgentGuard CLI - Session management and OpenTelemetry trace emission",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )

    # AgentGuard configuration arguments
    parser.add_argument(
        "--agent-id",
        default="hostname",
        help="Unique identifier for the agent",
    )

    parser.add_argument(
        "--deployment-id",
        default="local",
        help="Identifier for the deployment",
    )

    parser.add_argument(
        "--deployment-name",
        default="Local Dev",
        help="Human-readable name for the deployment",
    )

    parser.add_argument(
        "--endpoint",
        default="http://localhost:4318",
        help="OpenTelemetry endpoint URL",
    )

    # Positional commands
    subparsers = parser.add_subparsers(dest="command", required=True, help="Commands")

    # session_start command
    session_start_parser = subparsers.add_parser("session_start", help="Start a new agent session")
    session_start_parser.add_argument("session_id", help="Session identifier")
    session_start_parser.add_argument("run_id", nargs="?", help="Optional run identifier")

    # session_end command
    session_end_parser = subparsers.add_parser("session_end", help="End a session with status")
    session_end_parser.add_argument("session_id", help="Session identifier")
    session_end_parser.add_argument(
        "status", nargs="?", default="completed", help="Session status (default: completed)"
    )

    # session_trace command
    session_trace_parser = subparsers.add_parser(
        "session_trace", help="Create trace for an existing session"
    )
    session_trace_parser.add_argument("trace_id", help="Trace identifier")
    session_trace_parser.add_argument("resource_id", nargs="?", help="Optional resource identifier")

    args = parser.parse_args(argv)

    hooks = AgentGuardHooks(
        agent_id=args.agent_id,
        deployment_id=args.deployment_id,
        deployment_name=args.deployment_name,
        endpoint=args.endpoint,
    )

    try:
        if args.command == "session_start":
            start_ns = hooks.on_session_start(args.session_id, args.run_id)
            print(f"Started session: {args.session_id} (start_ns={start_ns})")

        elif args.command == "session_end":
            session_id = args.session_id

            if not session_id:
                print("Error: session_id required for session_end", file=sys.stderr)
                sys.exit(1)

            hooks.on_session_end(session_id, args.status)
            print(f"Ended session: {session_id} with status: {args.status}")

    except Exception as e:
        print(f"Error: {str(e)}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
