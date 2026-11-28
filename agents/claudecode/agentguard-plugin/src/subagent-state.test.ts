import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { mkdtempSync, rmSync, existsSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";
import { claimSubagentCalls, clearSubagentState } from "./subagent-state.js";

describe("claimSubagentCalls", () => {
  let dir: string;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "agentguard-state-"));
    process.env.AGENTGUARD_STATE_DIR = dir;
  });

  afterEach(() => {
    delete process.env.AGENTGUARD_STATE_DIR;
    rmSync(dir, { recursive: true, force: true });
  });

  it("returns null for the first report of a sub-agent", () => {
    expect(claimSubagentCalls("s1", "a1", 3)).toBeNull();
  });

  it("returns what was reported before, so a second hook sends nothing twice", () => {
    claimSubagentCalls("s1", "a1", 3);
    expect(claimSubagentCalls("s1", "a1", 3)).toBe(3);
  });

  it("lets a resumed sub-agent send only its new requests", () => {
    claimSubagentCalls("s1", "a1", 3);
    expect(claimSubagentCalls("s1", "a1", 5)).toBe(3);
    expect(claimSubagentCalls("s1", "a1", 5)).toBe(5);
  });

  it("keeps sub-agents and sessions apart", () => {
    claimSubagentCalls("s1", "a1", 3);
    expect(claimSubagentCalls("s1", "a2", 1)).toBeNull();
    expect(claimSubagentCalls("s2", "a1", 1)).toBeNull();
  });

  it("does not let an id leave the state directory", () => {
    expect(claimSubagentCalls("../../x", "../y", 1)).toBeNull();
    expect(existsSync(join(dir, ".._.._x", ".._y"))).toBe(true);
  });

  it("returns null without ids, and when the state cannot be written", () => {
    expect(claimSubagentCalls("", "a1", 1)).toBeNull();
    expect(claimSubagentCalls("s1", "", 1)).toBeNull();
    process.env.AGENTGUARD_STATE_DIR = "/dev/null/not-a-directory";
    expect(claimSubagentCalls("s1", "a1", 1)).toBeNull();
    expect(claimSubagentCalls("s1", "a1", 1)).toBeNull();
  });

  it("clearSubagentState forgets a session", () => {
    claimSubagentCalls("s1", "a1", 3);
    clearSubagentState("s1");
    expect(claimSubagentCalls("s1", "a1", 3)).toBeNull();
    expect(() => clearSubagentState("never-seen")).not.toThrow();
  });
});
