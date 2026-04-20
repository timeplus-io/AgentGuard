# agentguard-hermes

AgentGuard security monitoring plugin for [Hermes Agent](https://github.com/nousresearch/hermes-agent).

Forwards Hermes session, LLM, and tool-call events to an [AgentGuard](https://github.com/timeplus/agentguard) Timeplus stream for real-time observability and security monitoring.

## Installation

```bash
pip install agentguard-hermes-plugin
agentguard-hermes install
```

This copies the plugin into `~/.hermes/plugins/agentguard/` where Hermes auto-loads it on startup.

## Configuration

Set environment variables before starting Hermes:

| Variable | Default | Description |
|---|---|---|
| `AGENTGUARD_TIMEPLUS_URL` | `http://localhost:3218` | Timeplus Enterprise HTTP endpoint |
| `AGENTGUARD_USERNAME` | `proton` | Timeplus username |
| `AGENTGUARD_PASSWORD` | _(empty)_ | Timeplus password |
| `AGENTGUARD_AGENT_ID` | hostname | Identifier for this agent instance |
| `AGENTGUARD_DEPLOYMENT_ID` | `local` | Deployment environment tag |
| `AGENTGUARD_DEPLOYMENT_NAME` | `Local Dev` | Human-readable deployment name |
| `AGENTGUARD_STREAM` | `agentguard_hook_events` | Target Timeplus stream |

### Example

```bash
export AGENTGUARD_TIMEPLUS_URL=http://timeplus.example.com:3218
export AGENTGUARD_USERNAME=proton
export AGENTGUARD_PASSWORD=secret
export AGENTGUARD_DEPLOYMENT_ID=production
export AGENTGUARD_DEPLOYMENT_NAME="Production Hermes"

hermes
```

## What gets captured

Every Hermes hook event is forwarded to the `agentguard_hook_events` stream:

| Hermes hook | Sent as | Description |
|---|---|---|
| `on_session_start` | `on_session_start` | New session begins |
| `on_session_end` | `conversation_end` | Single chat turn completed |
| `on_session_finalize` | `on_session_end` | Session fully torn down (CLI exit, `/reset`) |
| `on_session_reset` | `on_session_reset` | Session rotated via `/new` |
| `pre_llm_call` | `pre_llm_call` | Before each LLM turn |
| `post_llm_call` | `post_llm_call` | After each LLM turn |
| `pre_api_request` | `pre_api_request` | Before each raw API call (token metrics) |
| `post_api_request` | `post_api_request` | After each raw API call |
| `pre_tool_call` | `pre_tool_call` | Before each tool execution |
| `post_tool_call` | `post_tool_call` | After each tool execution |

`conversation_history` is stripped from all events before ingestion — it is unbounded and contains no information not already available from the individual turn events.

## Custom install path

```bash
agentguard-hermes install --hermes-dir /path/to/hermes/data
```

## Manual installation (Makefile / Docker)

If you run Hermes via the provided Docker Compose setup in `agents/hermes/`:

```bash
make configure   # copies plugin into .hermes/plugins/agentguard/
make cli         # start Hermes CLI with AgentGuard env vars pre-set
make start       # start Hermes gateway + web dashboard
```

## Publishing to PyPI

```bash
pip install hatch
cd agents/hermes/agentguard-plugin
hatch build     # produces dist/agentguard_hermes_plugin-*.whl
hatch publish
```
