#!/usr/bin/env python3
"""
Codex hook command for AgentGuard.

See hooks.json for the hook configuration.

Codex hooks document: https://learn.chatgpt.com/docs/hooks
"""

from __future__ import annotations

import argparse
import sys
import json

from agentguard.hook import AgentGuardHooks


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser(
        description="AgentGuard CLI for Codex",
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

    args = parser.parse_args(argv)

    hooks = AgentGuardHooks(
        agent_id=args.agent_id,
        deployment_id=args.deployment_id,
        deployment_name=args.deployment_name,
        endpoint=args.endpoint,
    )

    try:
        event = json.loads(sys.stdin.read())
        event_name = event.get("hook_event_name")

        if event_name == "SessionStart":
            session_id = event["session_id"]
            hooks.on_session_start(session_id)

        elif event_name == "SessionEnd":
            session_id = event["session_id"]
            hooks.on_session_end(session_id)

        elif event_name == "UserPromptSubmit":
            session_id = event["session_id"]
            task_id = event["turn_id"]
            model = event["model"]
            prompt = event["prompt"]
            hooks.on_llm_end(session_id=session_id, task_id=task_id, model_name=model, user_message=prompt)

        elif event_name == "PreToolUse":
            session_id = event["session_id"]
            hooks.on_pre_tool(session_id=session_id)

        elif event_name == "PostToolUse":
            session_id = event["session_id"]
            tool_name = event["tool_name"]
            tool_input = event["tool_input"]
            tool_response = event["tool_response"]
            hooks.on_post_tool(session_id=session_id,
                               tool_name=tool_name,
                               tool_input=str(tool_input),
                               tool_result=str(tool_response))

    except Exception as e:
        print(f"Error: {str(e)}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
