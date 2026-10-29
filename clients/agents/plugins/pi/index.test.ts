import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import directions from "./index.ts";

type Handler = (event: any, ctx: any) => Promise<void>;

function harness() {
  const handlers = new Map<string, Handler>();
  const notifications: string[] = [];
  const calls: unknown[][] = [];
  let response = { code: 0, stdout: JSON.stringify({ root: "/repo", content: "DEFAULT_CONTEXT", warnings: [] }), stderr: "" };
  const api = {
    registerFlag(name: string, value: unknown) { assert.ok(["directions-bin", "directions-allow-urls"].includes(name)); },
    getFlag(name: string) { return name === "directions-bin" ? "/trusted/directions" : ""; },
    on(name: string, handler: Handler) { handlers.set(name, handler); },
    async exec(...args: unknown[]) { calls.push(args); return response; },
  };
  directions(api as unknown as Parameters<typeof directions>[0]);
  const ctx = { cwd: "/repo", hasUI: true, ui: { notify(message: string) { notifications.push(message); } } };
  return { api, handlers, ctx, notifications, calls, setResponse(value: typeof response) { response = value; } };
}

test("startup and per-prompt refresh preserve AGENTS and avoid accumulating context", async () => {
  const h = harness();
  assert.equal(h.calls.length, 0, "factory must not start a process");
  await h.handlers.get("session_start")!({}, h.ctx);
  const event = { systemPromptOptions: {
    sections: { other: "OTHER" } as Record<string, string>,
    contextFiles: [{ path: "/repo/AGENTS.md", content: "AGENTS_CONTENT" }],
  } };
  await h.handlers.get("before_agent_start")!(event, h.ctx);
  assert.equal(event.systemPromptOptions.sections.directions, "DEFAULT_CONTEXT");
  assert.equal(event.systemPromptOptions.sections.other, "OTHER");
  assert.equal(event.systemPromptOptions.contextFiles[0].content, "AGENTS_CONTENT");
  assert.deepEqual(h.calls[0], ["/trusted/directions", ["context", "/repo"], { cwd: "/repo", timeout: 10000 }]);
  h.setResponse({ code: 0, stdout: JSON.stringify({ root: "/repo", content: "NEW_DIFF_POLICY", warnings: [] }), stderr: "" });
  await h.handlers.get("before_agent_start")!(event, h.ctx);
  assert.equal(event.systemPromptOptions.sections.directions, "NEW_DIFF_POLICY");
  h.setResponse({ code: 0, stdout: JSON.stringify({ root: "", content: "", warnings: [] }), stderr: "" });
  await h.handlers.get("before_agent_start")!(event, h.ctx);
  assert.ok(!("directions" in event.systemPromptOptions.sections));
});

test("only host-provided URL approvals are forwarded", async () => {
  const h = harness();
  h.api.getFlag = (name) => name === "directions-bin" ? "/trusted/directions" : "https://example.invalid/a\nhttps://example.invalid/b";
  await h.handlers.get("session_start")!({}, h.ctx);
  assert.deepEqual(h.calls[0][1], ["context", "/repo", "--allow-url", "https://example.invalid/a", "--allow-url", "https://example.invalid/b"]);
});

test("invalid config/binary responses clear stale content and remain visible without a UI", async () => {
  const h = harness();
  const event = { systemPromptOptions: { sections: { directions: "STALE" } as Record<string, string> } };
  for (const response of [
    { code: 1, stdout: "", stderr: "invalid config" },
    { code: 0, stdout: "invalid JSON", stderr: "" },
    { code: 0, stdout: JSON.stringify({ root: "", content: "x", warnings: [null] }), stderr: "" },
    { code: 0, stdout: JSON.stringify({ root: "", content: "x".repeat(65537), warnings: [] }), stderr: "" },
  ]) {
    h.setResponse(response);
    await h.handlers.get("before_agent_start")!(event, { ...h.ctx, hasUI: false });
    assert.match(event.systemPromptOptions.sections.directions, /could not be loaded/);
    assert.ok(!event.systemPromptOptions.sections.directions.includes("STALE"));
  }
  h.api.exec = async () => { throw new Error("binary missing"); };
  await h.handlers.get("before_agent_start")!(event, h.ctx);
  assert.match(event.systemPromptOptions.sections.directions, /binary missing/);
});

test("warnings are included for headless consumers and notifications are deduplicated", async () => {
  const h = harness();
  h.setResponse({ code: 0, stdout: JSON.stringify({ root: "/repo", content: "CONTEXT", warnings: ["Diff unavailable"] }), stderr: "" });
  await h.handlers.get("session_start")!({}, h.ctx);
  const event = { systemPromptOptions: { sections: {} as Record<string, string> } };
  await h.handlers.get("before_agent_start")!(event, h.ctx);
  await h.handlers.get("before_agent_start")!(event, h.ctx);
  assert.equal(h.notifications.length, 1);
  assert.match(event.systemPromptOptions.sections.directions, /Diff unavailable/);
});

test("extension integrates with the real Go CLI, bootstrap config, and local Git diff", async () => {
  const pluginDir = dirname(fileURLToPath(import.meta.url));
  const root = resolve(pluginDir, "../../../..");
  const temp = mkdtempSync(join(tmpdir(), "directions-pi-test-"));
  try {
    const binary = join(temp, "directions");
    execFileSync("go", ["build", "-o", binary, "."], { cwd: join(root, "clients/cli") });
    const repo = join(temp, "repo");
    mkdirSync(repo);
    execFileSync("git", ["init", "-q"], { cwd: repo });
    execFileSync(join(root, "bootstrap.sh"), [repo]);
    // An untracked reusable prompt matches the starter's meta policy.
    const h = harness();
    h.api.getFlag = (name) => name === "directions-bin" ? binary : "";
    h.api.exec = async (command: unknown, args: unknown, options: unknown) => ({
      code: 0, stdout: execFileSync(command as string, args as string[], options as object).toString(), stderr: "",
    });
    const event = { systemPromptOptions: {
      sections: {} as Record<string, string>,
      contextFiles: [{ path: join(repo, "AGENTS.md"), content: "NATIVE_AGENT" }],
    } };
    await h.handlers.get("session_start")!({}, { ...h.ctx, cwd: repo });
    await h.handlers.get("before_agent_start")!(event, { ...h.ctx, cwd: repo });
    assert.match(event.systemPromptOptions.sections.directions, /General repository guidance/);
    assert.match(event.systemPromptOptions.sections.directions, /only-prompts-in-contexts-dir/);
    assert.equal(event.systemPromptOptions.contextFiles[0].content, "NATIVE_AGENT");
    // Stage everything and commit: after the worktree becomes clean, remove the policy.
    execFileSync("git", ["add", "."], { cwd: repo });
    execFileSync("git", ["-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "initial"], { cwd: repo });
    await h.handlers.get("before_agent_start")!(event, { ...h.ctx, cwd: repo });
    assert.ok(!event.systemPromptOptions.sections.directions.includes("only-prompts-in-contexts-dir"));
    const prompt = join(repo, ".directions/contexts/general.md");
    writeFileSync(prompt, readFileSync(prompt, "utf8") + "\nUpdated guidance.");
    await h.handlers.get("before_agent_start")!(event, { ...h.ctx, cwd: repo });
    assert.match(event.systemPromptOptions.sections.directions, /Updated guidance/);
    assert.match(event.systemPromptOptions.sections.directions, /only-prompts-in-contexts-dir/);
  } finally { rmSync(temp, { recursive: true, force: true }); }
});
