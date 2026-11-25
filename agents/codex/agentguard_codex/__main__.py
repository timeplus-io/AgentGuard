"""Allow ``python -m agentguard_codex`` to act as the hook command."""

from agentguard_codex.cli import main

raise SystemExit(main())
