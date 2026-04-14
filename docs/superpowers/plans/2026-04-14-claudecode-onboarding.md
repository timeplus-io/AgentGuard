# Claude Code Onboarding UI — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable the Claude Code agent card and add agent-branched content for steps 1 and 2 in the onboarding wizard.

**Architecture:** All changes are in one file — `frontend/src/pages/Onboarding.jsx`. The existing 4-step wizard structure is unchanged; we branch the rendered content inside steps 1 and 2 on `selectedAgent`. No backend changes needed.

**Tech Stack:** React 18, Tailwind CSS, existing `CodeBlock` / `CopyButton` components already in the file.

---

## Files

- Modify: `frontend/src/pages/Onboarding.jsx`

---

## Task 1: Enable Claude Code card + add `agentId` state

**Files:**
- Modify: `frontend/src/pages/Onboarding.jsx:12-29` (agentTypes), `:66-69` (state)

- [ ] **Step 1: Update the `agentTypes` array**

In `frontend/src/pages/Onboarding.jsx`, replace the `claude-code` entry (lines 21-28):

```js
  {
    id: 'claude-code',
    name: 'Claude Code',
    desc: "Anthropic's Claude Code CLI. Monitor tool use, LLM calls, cost, and security events in real time.",
    icon: 'code',
    badge: 'Supported',
    disabled: false,
  },
```

- [ ] **Step 2: Add `agentId` state below the existing Step 1 state block (line 69)**

```js
  const [agentId, setAgentId] = useState('')
```

- [ ] **Step 3: Verify in browser**

Run `cd frontend && npm run dev`, open `http://localhost:5173/onboarding`. The Claude Code card should be clickable, show "Supported" badge, and selecting it should highlight it pink. Clicking Next from step 0 should advance to step 1 (content will be OpenClaw-only for now — that's fine).

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/Onboarding.jsx
git commit -m "feat(onboarding): enable Claude Code agent card"
```

---

## Task 2: Add Claude Code computed code strings

**Files:**
- Modify: `frontend/src/pages/Onboarding.jsx` — add computed values after the existing `envVarsCode`/`jsonConfigCode` block (after line 128)

- [ ] **Step 1: Add the four computed strings for Claude Code**

Insert after the existing `otelConfigCode` block (after line 149), before the `handleNext` line:

```js
  // ── Claude Code computed snippets ──────────────────────────────────────────
  const hookEntry = { matcher: '*', hooks: [{ type: 'command', command: 'agentguard-hook', async: true }] }
  const ccSettingsJsonCode = JSON.stringify({
    hooks: {
      SessionStart:       [hookEntry],
      SessionEnd:         [hookEntry],
      UserPromptSubmit:   [hookEntry],
      PreToolUse:         [hookEntry],
      PostToolUse:        [hookEntry],
      PostToolUseFailure: [hookEntry],
      SubagentStart:      [hookEntry],
      SubagentStop:       [hookEntry],
      Stop:               [hookEntry],
      PermissionDenied:   [hookEntry],
    },
    env: {
      AGENTGUARD_AGENT_ID:        agentId || '<your-hostname>',
      AGENTGUARD_DEPLOYMENT_ID:   deploymentId,
      AGENTGUARD_DEPLOYMENT_NAME: deploymentName,
      AGENTGUARD_TIMEPLUS_URL:    timeplusUrl,
    },
  }, null, 2)

  const ccEnvCode =
    `export AGENTGUARD_AGENT_ID="${agentId || '<your-hostname>'}"\n` +
    `export AGENTGUARD_DEPLOYMENT_ID="${deploymentId}"\n` +
    `export AGENTGUARD_DEPLOYMENT_NAME="${deploymentName}"\n` +
    `export AGENTGUARD_TIMEPLUS_URL="${timeplusUrl}"`

  const ccOtelSettingsCode = JSON.stringify({
    env: {
      CLAUDE_CODE_ENABLE_TELEMETRY:        '1',
      CLAUDE_CODE_ENHANCED_TELEMETRY_BETA: '1',
      OTEL_METRICS_EXPORTER:               'otlp',
      OTEL_LOGS_EXPORTER:                  'otlp',
      OTEL_TRACES_EXPORTER:                'otlp',
      OTEL_EXPORTER_OTLP_PROTOCOL:         'http/protobuf',
      OTEL_EXPORTER_OTLP_ENDPOINT:         otelEndpoint,
      OTEL_METRIC_EXPORT_INTERVAL:         '10000',
      OTEL_LOGS_EXPORT_INTERVAL:           '5000',
      OTEL_LOG_TOOL_DETAILS:               '1',
    },
  }, null, 2)
```

- [ ] **Step 2: Verify no syntax errors**

```bash
cd frontend && npm run build 2>&1 | tail -20
```

Expected: build succeeds with no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/Onboarding.jsx
git commit -m "feat(onboarding): add Claude Code computed config snippets"
```

---

## Task 3: Branch Step 1 content for Claude Code

**Files:**
- Modify: `frontend/src/pages/Onboarding.jsx:300-350` (Step 1 block)

- [ ] **Step 1: Replace the entire Step 1 block**

Replace the block `{/* Step 1: Install Plugin */}` (lines 300–350) with the branched version below. The OpenClaw content is unchanged; the Claude Code branch is new.

```jsx
              {/* Step 1: Install Plugin */}
              {step === 1 && selectedAgent === 'openclaw' && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">Install Plugin</h2>
                    <p className="text-zinc-400 mt-1.5 text-xs leading-relaxed">Install the AgentGuard plugin into your OpenClaw project and configure it to point at your Timeplus instance.</p>
                  </div>

                  <CodeBlock label="1. Install via npm" code="npm install @timeplus/agentguard-openclaw-plugin" />

                  <div className="space-y-3">
                    <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">2. Configuration</p>
                    <div className="grid grid-cols-1 gap-3">
                      {[
                        { label: 'Deployment ID', val: deploymentId, set: setDeploymentId, ph: 'my-team' },
                        { label: 'Deployment Name', val: deploymentName, set: setDeploymentName, ph: 'My Team' },
                        { label: 'Timeplus URL', val: timeplusUrl, set: setTimeplusUrl, ph: 'http://localhost:3218' },
                      ].map(({ label, val, set, ph }) => (
                        <div key={label} className="flex items-center gap-4">
                          <label className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest w-36 shrink-0">{label}</label>
                          <input
                            value={val}
                            onChange={e => set(e.target.value)}
                            placeholder={ph}
                            className="flex-1 bg-[#0f0c17] text-xs py-2 px-3 rounded-sm focus:ring-1 focus:ring-[#ffb0cf] focus:outline-none text-[#e7dff0] font-mono"
                          />
                        </div>
                      ))}
                    </div>
                  </div>

                  <div className="space-y-2">
                    <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">3. Apply configuration</p>
                    <div className="flex gap-2 mb-2">
                      {[{ id: 'env', label: 'Env Variables' }, { id: 'json', label: 'openclaw.json' }].map(tab => (
                        <button
                          key={tab.id}
                          onClick={() => setConfigTab(tab.id)}
                          className={`px-3 py-1 text-[10px] font-semibold rounded-full transition-colors ${configTab === tab.id ? 'bg-[#ffb0cf]/10 text-[#ffb0cf]' : 'text-zinc-500 hover:text-zinc-300'}`}
                        >
                          {tab.label}
                        </button>
                      ))}
                    </div>
                    <CodeBlock
                      label={configTab === 'env' ? '.env / shell export' : 'plugins.entries.agentguard (openclaw.json)'}
                      code={configTab === 'env' ? envVarsCode : jsonConfigCode}
                    />
                  </div>
                </>
              )}

              {step === 1 && selectedAgent === 'claude-code' && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">Install Plugin</h2>
                    <p className="text-zinc-400 mt-1.5 text-xs leading-relaxed">Install the AgentGuard hook plugin globally, then add the generated config to your <code className="text-zinc-300">~/.claude/settings.json</code>.</p>
                  </div>

                  <CodeBlock label="1. Install via npm" code="npm install -g @timeplus/agentguard-claudecode-plugin" />

                  <div className="space-y-3">
                    <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">2. Configuration</p>
                    <div className="grid grid-cols-1 gap-3">
                      {[
                        { label: 'Agent ID *', val: agentId, set: setAgentId, ph: 'your-hostname' },
                        { label: 'Deployment ID', val: deploymentId, set: setDeploymentId, ph: 'local' },
                        { label: 'Deployment Name', val: deploymentName, set: setDeploymentName, ph: 'Local Dev' },
                        { label: 'Timeplus URL', val: timeplusUrl, set: setTimeplusUrl, ph: 'http://localhost:3218' },
                      ].map(({ label, val, set, ph }) => (
                        <div key={label} className="flex items-center gap-4">
                          <label className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest w-36 shrink-0">{label}</label>
                          <input
                            value={val}
                            onChange={e => set(e.target.value)}
                            placeholder={ph}
                            className="flex-1 bg-[#0f0c17] text-xs py-2 px-3 rounded-sm focus:ring-1 focus:ring-[#ffb0cf] focus:outline-none text-[#e7dff0] font-mono"
                          />
                        </div>
                      ))}
                    </div>
                    <p className="text-[10px] text-zinc-500 leading-relaxed">* Agent ID identifies this Claude Code instance — use your hostname or machine name.</p>
                  </div>

                  <div className="space-y-2">
                    <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">3. Add to ~/.claude/settings.json</p>
                    <div className="flex gap-2 mb-2">
                      {[{ id: 'json', label: 'settings.json' }, { id: 'env', label: 'Shell env' }].map(tab => (
                        <button
                          key={tab.id}
                          onClick={() => setConfigTab(tab.id)}
                          className={`px-3 py-1 text-[10px] font-semibold rounded-full transition-colors ${configTab === tab.id ? 'bg-[#ffb0cf]/10 text-[#ffb0cf]' : 'text-zinc-500 hover:text-zinc-300'}`}
                        >
                          {tab.label}
                        </button>
                      ))}
                    </div>
                    <CodeBlock
                      label={configTab === 'json' ? '~/.claude/settings.json — merge this block' : 'Shell profile (.zshrc / .bashrc)'}
                      code={configTab === 'json' ? ccSettingsJsonCode : ccEnvCode}
                    />
                    {configTab === 'json' && (
                      <p className="text-[10px] text-zinc-500 leading-relaxed">Merge with your existing <code className="text-zinc-400">~/.claude/settings.json</code> — do not replace the whole file.</p>
                    )}
                  </div>
                </>
              )}
```

- [ ] **Step 2: Verify in browser**

With dev server running, select Claude Code on step 0, advance to step 1. Verify:
- Install command shows `npm install -g @timeplus/agentguard-claudecode-plugin`
- Four fields are shown (Agent ID, Deployment ID, Deployment Name, Timeplus URL)
- Default tab is `settings.json`, shows the hooks + env JSON block
- Switching to "Shell env" tab shows 4 `export` lines
- Editing any field live-updates the code snippet
- Switching back to OpenClaw on step 0 then re-entering step 1 shows the original OpenClaw content unchanged

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/Onboarding.jsx
git commit -m "feat(onboarding): add Claude Code step 1 install + config UI"
```

---

## Task 4: Branch Step 2 content for Claude Code

**Files:**
- Modify: `frontend/src/pages/Onboarding.jsx:352-383` (Step 2 block)

- [ ] **Step 1: Replace the entire Step 2 block**

Replace `{/* Step 2: OTel Config */}` (lines 352–383) with the branched version:

```jsx
              {/* Step 2: OTel Config */}
              {step === 2 && selectedAgent === 'openclaw' && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">
                      OpenTelemetry Config
                      <span className="ml-2 text-[10px] font-semibold px-2 py-0.5 rounded-full bg-[#37333f] text-zinc-500 align-middle">Optional</span>
                    </h2>
                    <p className="text-zinc-400 mt-1.5 text-xs leading-relaxed">
                      Enable the built-in OTel diagnostics plugin to export traces, metrics, and logs from OpenClaw to Timeplus. Recommended for full observability, but you can skip this step.
                    </p>
                  </div>

                  <div className="space-y-3">
                    <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Timeplus OTLP Endpoint</p>
                    <div className="flex items-center gap-4">
                      <label className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest w-36 shrink-0">Endpoint URL</label>
                      <input
                        value={otelEndpoint}
                        onChange={e => setOtelEndpoint(e.target.value)}
                        placeholder="http://host.docker.internal:4318"
                        className="flex-1 bg-[#0f0c17] text-xs py-2 px-3 rounded-sm focus:ring-1 focus:ring-[#ffb0cf] focus:outline-none text-[#e7dff0] font-mono"
                      />
                    </div>
                    <p className="text-[10px] text-zinc-500 leading-relaxed pl-40">
                      Use <code className="text-zinc-400">host.docker.internal</code> when OpenClaw runs in Docker and Timeplus runs on the host. Replace with the actual host/IP if needed.
                    </p>
                  </div>

                  <CodeBlock label="openclaw.json — diagnostics section" code={otelConfigCode} />
                </>
              )}

              {step === 2 && selectedAgent === 'claude-code' && (
                <>
                  <div>
                    <h2 className="text-xl font-semibold text-white tracking-tight">
                      OpenTelemetry Config
                      <span className="ml-2 text-[10px] font-semibold px-2 py-0.5 rounded-full bg-[#37333f] text-zinc-500 align-middle">Optional</span>
                    </h2>
                    <p className="text-zinc-400 mt-1.5 text-xs leading-relaxed">
                      Enable Claude Code's built-in OTel telemetry to export LLM metrics and logs to Timeplus. This unlocks token usage, cost, and latency data. You can skip this step — hook events will still be captured.
                    </p>
                  </div>

                  <div className="space-y-3">
                    <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Timeplus OTLP Endpoint</p>
                    <div className="flex items-center gap-4">
                      <label className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest w-36 shrink-0">Endpoint URL</label>
                      <input
                        value={otelEndpoint}
                        onChange={e => setOtelEndpoint(e.target.value)}
                        placeholder="http://localhost:4318"
                        className="flex-1 bg-[#0f0c17] text-xs py-2 px-3 rounded-sm focus:ring-1 focus:ring-[#ffb0cf] focus:outline-none text-[#e7dff0] font-mono"
                      />
                    </div>
                  </div>

                  <CodeBlock label="~/.claude/settings.json — merge this env block" code={ccOtelSettingsCode} />
                  <p className="text-[10px] text-zinc-500 leading-relaxed">
                    Merge the <code className="text-zinc-400">env</code> keys into your existing <code className="text-zinc-400">~/.claude/settings.json</code>. The <code className="text-zinc-400">http/protobuf</code> protocol is required — do not use gRPC.
                  </p>
                </>
              )}
```

- [ ] **Step 2: Verify in browser**

With dev server running, select Claude Code, advance to step 2. Verify:
- Heading shows "OpenTelemetry Config" with "Optional" badge
- Description mentions Claude Code and hook events fallback
- Endpoint field defaults to `http://localhost:4318` (derived from `timeplusUrl`)
- Editing the endpoint live-updates the JSON snippet
- Code block shows the 10-key `env` block with `http/protobuf`
- Note below mentions merging and not using gRPC
- Skip button still works (advances to step 3)
- Selecting OpenClaw and advancing to step 2 still shows the original OpenClaw OTel content

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/Onboarding.jsx
git commit -m "feat(onboarding): add Claude Code step 2 OTel config UI"
```

---

## Task 5: Reset `configTab` default when agent changes + final push

The `configTab` state defaults to `'env'` which is the second tab for Claude Code (Claude Code's first tab is `'json'`). When the user selects Claude Code, the tab should default to `'json'`.

**Files:**
- Modify: `frontend/src/pages/Onboarding.jsx` — add a `useEffect` after the existing state declarations

- [ ] **Step 1: Add a `useEffect` to reset `configTab` when `selectedAgent` changes**

Add after the `configTab` state declaration (after line 69):

```js
  useEffect(() => {
    setConfigTab(selectedAgent === 'claude-code' ? 'json' : 'env')
  }, [selectedAgent])
```

- [ ] **Step 2: Verify end-to-end in browser**

Full walkthrough — Claude Code path:
1. Open `/onboarding`
2. Select Claude Code → card highlights
3. Click Next → step 1 shows `settings.json` tab selected by default
4. Fill in Agent ID `my-mac`, Deployment ID `dev`, Timeplus URL `http://localhost:3218`
5. Verify `ccSettingsJsonCode` updates live with the new values
6. Switch to "Shell env" tab → shows 4 exports with correct values
7. Click Next → step 2 shows OTel block with endpoint `http://localhost:4318`
8. Edit endpoint → JSON snippet updates
9. Click Skip → step 3 rules catalog loads
10. Toggle a rule → click Activate Agent → success screen appears

Full walkthrough — OpenClaw path (regression check):
1. Refresh, select OpenClaw, click through all steps — everything should work as before

- [ ] **Step 3: Build check**

```bash
cd frontend && npm run build 2>&1 | tail -5
```

Expected: `✓ built in X.XXs` with no errors.

- [ ] **Step 4: Commit and push**

```bash
git add frontend/src/pages/Onboarding.jsx
git commit -m "feat(onboarding): reset config tab default when agent changes"
git push
```
