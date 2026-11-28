import { mkdirSync, readFileSync, rmSync, writeFileSync } from "fs";
import { tmpdir } from "os";
import { join } from "path";

// A hook is one short-lived process, so what was already reported for a
// sub-agent is kept in a small file per sub-agent:
//   <tmp>/agentguard-claudecode/<session_id>/<agent_id>   → number of requests sent
// Two hooks can report the same sub-agent: SubagentStop, and the PostToolUse of
// the Agent tool call for sub-agents that fire no SubagentStop. A sub-agent can
// also be resumed and stop again. The file keeps both from sending a request twice.

function safe(id: string): string {
  return id.replace(/[^A-Za-z0-9_.-]/g, "_");
}

function sessionDir(sessionId: string): string {
  return join(process.env.AGENTGUARD_STATE_DIR || join(tmpdir(), "agentguard-claudecode"), safe(sessionId));
}

/**
 * Records that `total` model requests of a sub-agent are being reported and
 * returns how many were reported before: `null` when this is the first report
 * for the sub-agent. Never throws; when the state cannot be read or written the
 * answer is `null`, so the caller sends everything.
 */
export function claimSubagentCalls(sessionId: string, agentId: string, total: number): number | null {
  if (!sessionId || !agentId) return null;
  const dir = sessionDir(sessionId);
  const file = join(dir, safe(agentId));
  try {
    mkdirSync(dir, { recursive: true });
    try {
      // "wx" fails if the file exists, so of two hooks racing only one is first.
      writeFileSync(file, String(total), { flag: "wx" });
      return null;
    } catch {
      const before = parseInt(readFileSync(file, "utf8"), 10);
      const sent = Number.isFinite(before) ? before : total;
      if (total > sent) writeFileSync(file, String(total));
      return sent;
    }
  } catch {
    return null;
  }
}

/** Removes the state of a session. Called on SessionEnd. */
export function clearSubagentState(sessionId: string): void {
  if (!sessionId) return;
  try {
    rmSync(sessionDir(sessionId), { recursive: true, force: true });
  } catch {
    // best-effort
  }
}
