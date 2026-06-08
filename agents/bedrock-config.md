# Running Claude Code, Hermes Agent & OpenClaw on Amazon Bedrock

A step-by-step guide to setting up three AI agent tools against the same
Amazon Bedrock–hosted Claude models. Because all three authenticate to
Bedrock the same way, the AWS-side work is done **once** and reused by each
tool.

| Tool | What it is | Language | Config file |
|------|------------|----------|-------------|
| **Claude Code** | Anthropic's agentic coding CLI / IDE tool | Node.js | `~/.claude/settings.json` + env vars |
| **Hermes Agent** | Nous Research self-improving personal agent | Python | `~/.hermes/config.yaml` |
| **OpenClaw** | Self-hosted multi-channel personal assistant | Node.js | `~/.openclaw/openclaw.json` |

---

## Part 0 — Shared Foundation (do this once)

Every tool below just calls Claude models on Bedrock, so complete these two
steps first.

### Step 0.1 — Enable Anthropic model access in Bedrock

1. Sign in to the **AWS Console** and choose your region (e.g. `us-east-1`).
2. Open **Amazon Bedrock → Model catalog**.
3. Select an Anthropic model (Opus 4.6 / Sonnet 4.6 / Haiku 4.5).
4. Submit the **use-case / First-Time-Use form**. Access is typically granted
   immediately. This is a one-time, per-account step.

> **Model IDs.** Bedrock references models by *cross-region inference profile*,
> e.g. `us.anthropic.claude-opus-4-6-v1:0` or
> `us.anthropic.claude-sonnet-4-6-v1:0`. The `us.` / `eu.` / `apac.` prefix
> controls where requests are routed for data-residency purposes. In non-US
> regions you must swap in the matching regional profile ID.

### Step 0.2 — Set up AWS credentials (pick ONE)

All three tools accept the same auth methods:

| Method | Best for | How |
|--------|----------|-----|
| **IAM instance role** | EC2 / Lightsail | Attach a role with `AmazonBedrockFullAccess` (or scoped invoke perms). No keys needed — tools auto-detect it. |
| **AWS profile** | Local dev with SSO | `aws configure` or `aws sso login`, then set `AWS_PROFILE`. |
| **Access keys** | Quick local testing | `export AWS_ACCESS_KEY_ID=...` / `AWS_SECRET_ACCESS_KEY=...` |
| **Bedrock API key** | Simplest token-based auth | `export AWS_BEARER_TOKEN_BEDROCK=...` |

Minimum IAM permissions are `bedrock:InvokeModel` and
`bedrock:InvokeModelWithResponseStream` on the model/inference-profile ARNs
you plan to use.

### Step 0.3 — (Optional) Set a budget guardrail

All three tools bill **straight to your AWS account per token**. In
**AWS Budgets**, create a monthly budget scoped to Bedrock with alerts at
50% / 80% / 100% before you start.

---

## Part 1 — Claude Code

Anthropic's agentic coding tool that reads your codebase, edits files, and runs
commands from the terminal or IDE.

### Step 1.1 — Install

```bash
npm install -g @anthropic-ai/claude-code
```

(Requires Node.js. Verify with `node --version`.)

### Step 1.2 — Point it at Bedrock

**Option A — environment variables** (quickest):

```bash
export CLAUDE_CODE_USE_BEDROCK=1
export AWS_REGION=us-east-1
# Optional: pin specific models
export ANTHROPIC_MODEL='us.anthropic.claude-opus-4-6-v1:0'
export ANTHROPIC_FAST_MODEL='us.anthropic.claude-haiku-4-5'
```

**Option B — `~/.claude/settings.json`** (better for teams, also used by the
VS Code / IDE extensions):

```json
{
  "env": {
    "CLAUDE_CODE_USE_BEDROCK": "1",
    "AWS_REGION": "us-east-1",
    "ANTHROPIC_MODEL": "us.anthropic.claude-opus-4-6-v1:0"
  }
}
```

**Option C — login wizard.** If you have AWS credentials available, just run
`claude` and use the "Sign in with Bedrock" wizard — it walks you through model
selection and can pin a 1M-context option.

### Step 1.3 — Run

```bash
cd your-project
claude
```

### Step 1.4 — (Optional) Hardening & extras

- **Switch models:** change `ANTHROPIC_MODEL` (ensure access is enabled first).
- **Service tier:** set `ANTHROPIC_BEDROCK_SERVICE_TIER` to `default`, `flex`,
  or `priority` to trade cost vs. latency.
- **Guardrails:** create a Bedrock Guardrail, publish a version, and add the
  Guardrail headers to `settings.json` (enable cross-region inference on the
  Guardrail if you use cross-region profiles).
- **Ignore files:** add a `.claudeignore` to skip `node_modules`, build
  artifacts, and large data files.

📄 Docs: <https://code.claude.com/docs/en/amazon-bedrock>

---

## Part 2 — Hermes Agent (Nous Research)

An open-source, self-improving Python agent that builds skills from experience
and keeps persistent memory across sessions.

### Step 2.1 — Install with Bedrock support

```bash
pip install hermes-agent[bedrock]
```

### Step 2.2 — Select Bedrock as the provider

```bash
hermes model
# → choose "More providers..."
# → choose "AWS Bedrock"
# → select your region and model
```

This writes `~/.hermes/config.yaml`:

```yaml
model:
  default: us.anthropic.claude-sonnet-4-6
  provider: bedrock
  base_url: https://bedrock-runtime.us-east-2.amazonaws.com
bedrock:
  region: us-east-2
```

> **Region precedence** (highest first): `bedrock.region` in `config.yaml` →
> `AWS_REGION` → `AWS_DEFAULT_REGION` → default `us-east-1`.

### Step 2.3 — Start chatting

```bash
hermes chat
```

### Step 2.4 — (Optional) Guardrails & EC2 auto-auth

- **Guardrails:** add to `config.yaml`:

  ```yaml
  bedrock:
    region: us-east-2
    guardrail:
      guardrail_identifier: "abc123def456"
      guardrail_version: "1"
  ```

- **On EC2:** attach an IAM role with Bedrock access — Hermes detects the
  instance role automatically; no `.env` needed.
- **One-click EC2 deploy:** the `sample-hermes-agent-on-aws-with-bedrock`
  CloudFormation sample provisions the VPC, IAM role, and instance for you.

📄 Docs: <https://hermes-agent.nousresearch.com/docs/guides/aws-bedrock>

---

## Part 3 — OpenClaw

A self-hosted autonomous assistant that bridges chat apps
(WhatsApp / Telegram / Discord) to AI agents.

### Step 3.1 — Get OpenClaw running

**Easiest path — Amazon Lightsail blueprint:**

1. Launch the **OpenClaw blueprint** from the Amazon Lightsail console.
2. It ships **pre-configured with Amazon Bedrock as the default provider** —
   no extra model config needed.
3. Pair your browser / messaging channel and start chatting.

**Self-managed (e.g. your own EC2 / VPS):** install OpenClaw, then configure
Bedrock manually (next step).

### Step 3.2 — Configure Bedrock manually

Edit `~/.openclaw/openclaw.json`:

```json
{
  "models": {
    "providers": {
      "amazon-bedrock": {
        "baseUrl": "https://bedrock-runtime.us-east-1.amazonaws.com",
        "api": "bedrock-converse-stream",
        "auth": "aws-sdk",
        "models": [
          {
            "id": "us.anthropic.claude-opus-4-6-v1:0",
            "name": "Claude Opus 4.6 (Bedrock)",
            "reasoning": true,
            "input": ["text", "image"],
            "contextWindow": 200000,
            "maxTokens": 8192
          }
        ]
      }
    }
  },
  "agents": {
    "defaults": {
      "model": { "primary": "amazon-bedrock/us.anthropic.claude-opus-4-6-v1:0" }
    }
  }
}
```

### Step 3.3 — Verify & manage

```bash
openclaw models list      # confirm Bedrock models are discovered
openclaw config           # manage settings, switch models
```

### Step 3.4 — Auth notes

- With **env markers** (`AWS_ACCESS_KEY_ID`, `AWS_PROFILE`, or
  `AWS_BEARER_TOKEN_BEDROCK`), OpenClaw auto-enables the implicit Bedrock
  provider for model discovery without extra config.
- ⚠️ **EC2 IAM-role caveat:** when using IMDS (instance metadata), OpenClaw
  **cannot** detect AWS auth from env markers alone — you must **explicitly
  opt in** to the Bedrock provider in config.
- 🔒 **Security:** never expose the OpenClaw gateway to the open internet. The
  gateway auth token is your password — rotate it often and store it in your
  environment file, not hardcoded in the config.

📄 Docs: <https://docs.openclaw.ai/providers/bedrock>

---

## Part 4 — Running all three together

You can host all three on a single box (e.g. an EC2 `t4g.xlarge`) sharing one
IAM role and the same Bedrock backend:

1. Complete **Part 0** once and attach a single IAM role to the instance.
2. Install each tool (Parts 1–3). They don't conflict — different runtimes
   (Node/Python), different config files, different ports.
3. Point each at the same model IDs.
4. If connecting messaging channels, give OpenClaw and Hermes **separate**
   accounts/tokens to avoid collisions.

### When to choose what

| Need | Pick |
|------|------|
| Coding in the terminal / IDE | **Claude Code** |
| Self-improving personal agent with persistent memory | **Hermes Agent** |
| Always-on assistant wired to WhatsApp/Telegram/Discord | **OpenClaw** |
| Serverless / per-user isolation | OpenClaw or Hermes **AgentCore** samples (Firecracker microVMs per session) |

---

## Troubleshooting

| Symptom | Likely cause / fix |
|---------|--------------------|
| `AccessDeniedException` / auth error | Model access not enabled, or IAM lacks `bedrock:InvokeModel*`. Re-check Step 0.1 / 0.2. |
| Model "not found" in non-US region | Swap to the regional inference-profile ID (`eu.`/`apac.`). |
| Throttling / rate-limit errors | Request a quota increase in **AWS Service Quotas**; tools retry with backoff. |
| OpenClaw on EC2 not finding creds | Explicitly opt in to the Bedrock provider (IMDS caveat, Step 3.4). |
| Runaway cost | Set **AWS Budgets** alerts (Step 0.3); start on Sonnet, escalate to Opus only when needed. |

---

## Reference links

- Claude Code on Bedrock — <https://code.claude.com/docs/en/amazon-bedrock>
- Hermes Agent on Bedrock — <https://hermes-agent.nousresearch.com/docs/guides/aws-bedrock>
- OpenClaw Bedrock provider — <https://docs.openclaw.ai/providers/bedrock>
- OpenClaw on Lightsail (AWS) — <https://aws.amazon.com/blogs/aws/introducing-openclaw-on-amazon-lightsail-to-run-your-autonomous-private-ai-agents>
- Bedrock cross-region inference profiles — Amazon Bedrock documentation

> ⚠️ Model IDs, region availability, and pricing change frequently. Verify
> current model IDs in the Bedrock Model catalog before deploying.