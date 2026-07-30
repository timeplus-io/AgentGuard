#!/usr/bin/env python3
"""
AgentGuard CLI - Session management and OpenTelemetry trace emission

Usage:
    agentguard {session_start, session_end, prompt, pre_tool, post_tool}

Examples:
    agentguard --agent-id my-agent --deployment-id prod --deployment-name "Production" session_start
    agentguard --endpoint http://localhost:4318 --deployment-id dev session_end


Claude Code hook configuration: settings.json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "",
        "hooks": [ {"type": "command", "command": "python examples/claude_code/main.py session_start"} ]
      }
    ],
    "SessionEnd": [
      {
        "matcher": "",
        "hooks": [ {"type": "command", "command": "python examples/claude_code/main.py session_end"} ]
      }
    ],
    "UserPromptSubmit": [
      {
        "hooks": [ { "type": "command", "command": "python examples/claude_code/main.py prompt"} ]
      }
    ],
    "PreToolUse": [
      {
        "hooks": [ { "type": "command", "command": "python examples/claude_code/main.py pre_tool"} ]
      }
    ],
    "PostToolUse": [
      {
        "hooks": [ { "type": "command", "command": "python examples/claude_code/main.py post_tool"} ]
      }
    ]
  }
}

"""

from __future__ import annotations

import argparse
import json
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
    subparsers.add_parser("session_start", help="Start a new agent session")
    subparsers.add_parser("session_end", help="End a session")
    subparsers.add_parser("prompt", help="Submit prompt")
    subparsers.add_parser("pre_tool", help="Pre tool use")
    subparsers.add_parser("post_tool", help="Post tool use")

    args = parser.parse_args(argv)

    hooks = AgentGuardHooks(
        agent_id=args.agent_id,
        deployment_id=args.deployment_id,
        deployment_name=args.deployment_name,
        endpoint=args.endpoint,
    )

    try:
        if args.command == "session_start":
            data = json.loads(sys.stdin.read())
            session_id = data["session_id"]
            hooks.on_session_start(session_id)

        elif args.command == "session_end":
            data = json.loads(sys.stdin.read())
            session_id = data["session_id"]
            hooks.on_session_end(session_id)

        elif args.command == "prompt":
            data = json.loads(sys.stdin.read())
            hooks.on_llm_end(session_id=data["session_id"], user_message=data["prompt"])

        elif args.command == "pre_tool":
            data = json.loads(sys.stdin.read())
            hooks.on_pre_tool(session_id=data["session_id"])

        elif args.command == "post_tool":
            data = json.loads(sys.stdin.read())
            hooks.on_post_tool(session_id=data["session_id"],
                               tool_name=str(data["tool_name"]),
                               tool_input=str(data["tool_input"]),
                               tool_result=str(data["tool_response"]))

    except Exception as e:
        print(f"Error: {str(e)}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
