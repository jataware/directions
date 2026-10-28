import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createServer } from "node:http";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import * as sdk from "@earendil-works/pi-coding-agent";
import { DEFAULT_MODEL, configureModel, extensionPaths, inputs, redact, runAgent } from "./runner.mjs";
import { createGitHubIntegration } from "../tools/pi/github/index.mjs";
import { githubFixture } from "./fixtures/github.mjs";

const project = resolve(dirname(fileURLToPath(import.meta.url)), "..");

async function fixture() {
  const root = await mkdtemp(join(tmpdir(), "directions-agent-test-"));
  const workspace = join(root, "checkout"), output = join(root, "output");
  await mkdir(workspace); await mkdir(output);
  const bin = join(root, "directions");
  execFileSync("go", ["build", "-o", bin, "."], { cwd: join(project, "clients/cli") });
  await mkdir(join(workspace, ".directions/contexts"), { recursive: true });
  await writeFile(join(workspace, ".directions/base.toml"), `meta.version="v1.0.0"
[repo]
vcs="none"
[profiles.review]
prompt="contexts/review.md"
[[profiles.review.whitelist]]
group="code"
[policies.code.source]
title="Source policy"
instructions="TRIGGERED_DIFF_POLICY"
triggers.diff=["src/**/*.ts"]
`);
  await writeFile(join(workspace, ".directions/contexts/review.md"), "DIRECTIONS_CONTEXT");
  await writeFile(join(workspace, "AGENTS.md"), "NATIVE_AGENTS_CONTEXT");
  await mkdir(join(workspace, ".pi/extensions"), { recursive: true });
  await writeFile(join(workspace, ".pi/extensions/poison.ts"), `throw new Error("Repository extension must not load");`);
  await writeFile(join(workspace, ".pi/SYSTEM.md"), "MUST_NOT_LOAD_SYSTEM");
  await writeFile(join(workspace, ".pi/settings.json"), JSON.stringify({ packages: ["npm:do-not-install-this-package"] }));
  const eventPath = join(root, "event.json");
  await writeFile(eventPath, JSON.stringify({ pull_request: { number: 7 } }));
  const config = inputs({ INPUT_TASK: "Review the PR and retain a proposal.", INPUT_MODEL: "fixture/model", INPUT_API_KEY: "fake-model-key",
    INPUT_GITHUB_TOKEN: "fake-github-key", INPUT_GITHUB_CAPABILITIES: "read,review-propose", INPUT_PROFILE: "review",
    INPUT_WORKSPACE: workspace, DIRECTIONS_BIN: bin, RUNNER_TEMP: root, GITHUB_REPOSITORY: "owner/repo", GITHUB_ACTOR: "maintainer",
    GITHUB_EVENT_NAME: "pull_request", GITHUB_EVENT_PATH: eventPath });
  return { root, workspace, output, config, async close() { await rm(root, { recursive: true, force: true }); } };
}

async function modelServer(handler) {
  const errors = [], requests = [];
  const server = createServer(async (req, res) => {
    try {
      assert.equal(req.url, "/v1/chat/completions");
      assert.equal(req.headers.authorization, "Bearer fake-model-key");
      let body = ""; for await (const chunk of req) body += chunk;
      const json = JSON.parse(body); requests.push(json);
      const reply = await handler(json, requests.length);
      if (reply.status) { res.writeHead(reply.status); res.end(JSON.stringify({ error: { message: "fixture provider failure" } })); return; }
      res.writeHead(200, { "Content-Type": "text/event-stream" });
      res.end("data: " + JSON.stringify({ id: "fixture", object: "chat.completion.chunk", created: 1, model: "model",
        choices: [{ index: 0, delta: { role: "assistant", ...reply.delta }, finish_reason: reply.finish || "stop" }] }) + "\n\ndata: [DONE]\n\n");
    } catch (error) { errors.push(error); res.writeHead(500); res.end("fixture failed"); }
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const origin = `http://127.0.0.1:${server.address().port}/v1`;
  return { requests, errors,
    async configureModel(config, agentDir) {
      const runtime = await sdk.ModelRuntime.create({ authPath: join(agentDir, "auth.json"), modelsPath: null, allowModelNetwork: false, refreshOnCreate: false });
      runtime.registerProvider("fixture", { baseUrl: origin, api: "openai-completions", models: [{
        id: "model", name: "Fixture", reasoning: false, input: ["text"], contextWindow: 128000, maxTokens: 4096,
        cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
      }] });
      await runtime.setRuntimeApiKey("fixture", config.apiKey);
      return { modelRuntime: runtime, model: runtime.getModel("fixture", "model") };
    },
    async close() { server.closeAllConnections(); await new Promise((resolve) => server.close(resolve)); },
  };
}

function tool(name, args, id) {
  return { delta: { tool_calls: [{ index: 0, id, type: "function", function: { name, arguments: JSON.stringify(args) } }] }, finish: "tool_calls" };
}

test("inputs reject unsafe events/limits; redaction and approved extension paths are explicit", async () => {
  const basic = { INPUT_TASK: "Task", INPUT_MODEL: "openai/example" };
  const defaults = inputs({ INPUT_TASK: "Task" });
  assert.equal(DEFAULT_MODEL, "openai/gpt-6.1-sol");
  assert.equal(defaults.model, DEFAULT_MODEL);
  assert.equal(defaults.provider, "openai");
  assert.equal(defaults.modelID, "gpt-6.1-sol");
  assert.equal(inputs({ INPUT_TASK: "Task", INPUT_MODEL: "" }).model, DEFAULT_MODEL);
  for (const change of [{ GITHUB_EVENT_NAME: "pull_request_target" }, { INPUT_MAX_TURNS: "0" }, { INPUT_TIMEOUT_SECONDS: "2000" }, { INPUT_MODEL: "not-a-ref" }, { INPUT_THINKING: "unknown" }, { INPUT_GITHUB_CAPABILITIES: "all" }]) assert.throws(() => inputs({ ...basic, ...change }));
  assert.equal(redact("prefix abcdef abc", ["abc", "abcdef"]), "prefix [REDACTED] [REDACTED]");
  const f = await fixture();
  try {
    await assert.rejects(extensionPaths(["relative.ts"], f.workspace));
    await assert.rejects(extensionPaths([join(f.workspace, "AGENTS.md")], f.workspace));
  } finally { await f.close(); }
});

test("default Sol model resolves in the pinned catalog offline", async () => {
  const root = await mkdtemp(join(tmpdir(), "directions-default-model-"));
  try {
    const config = inputs({ INPUT_TASK: "Configure only", INPUT_API_KEY: "fake-model-key" });
    const { model, modelRuntime } = await configureModel(config, root);
    assert.equal(model.provider + "/" + model.id, DEFAULT_MODEL);
    assert.equal(modelRuntime.hasConfiguredAuth("openai"), true);
  } finally { await rm(root, { recursive: true, force: true }); }
});

test("real pinned SDK loads only approved ordinary extensions; native and Directions guidance use authoritative PR diff", async () => {
  const f = await fixture(), github = githubFixture();
  const trusted = join(f.root, "trusted-extension");
  await mkdir(trusted);
  await writeFile(join(trusted, "package.json"), JSON.stringify({ pi: { extensions: ["./index.mjs"] } }));
  await writeFile(join(trusted, "index.mjs"), `import { Type } from "typebox";
export default function(pi) {
  pi.on("before_agent_start", event => { event.systemPromptOptions.sections.external = "EXTERNAL_EXTENSION"; });
  pi.registerTool({ name: "approved_echo", label: "Echo", description: "Approved external tool", parameters: Type.Object({}),
    async execute() { return {content:[{type:"text",text:"EXTERNAL_TOOL_RESULT"}],details:undefined}; }
  });
}`);
  f.config.extensions = [trusted];
  const server = await modelServer((request, count) => {
    const text = JSON.stringify(request.messages);
    assert.match(text, /DIRECTIONS_CONTEXT/);
    assert.match(text, /NATIVE_AGENTS_CONTEXT/);
    assert.match(text, /TRIGGERED_DIFF_POLICY/);
    assert.match(text, /EXTERNAL_EXTENSION/);
    assert.ok(!text.includes("MUST_NOT_LOAD_SYSTEM"));
    assert.ok(!text.includes("fake-github-key"));
    assert.ok(!text.includes("fake-model-key"));
    assert.deepEqual(request.tools.map((tool) => tool.function.name).sort(), ["approved_echo", "github_propose_review", "github_read_discussion", "github_read_file", "github_read_pr"]);
    if (count === 1) return tool("approved_echo", {}, "external");
    assert.match(text, /EXTERNAL_TOOL_RESULT/);
    if (count === 2) return tool("github_read_pr", {}, "read");
    if (count === 3) return tool("github_propose_review", { body: "Validated proposed review." }, "propose");
    return { delta: { content: "Review proposed, not published. fake-model-key fake-github-key" } };
  });
  try {
    const result = await runAgent(f.config, f.output, { configureModel: server.configureModel,
      createGitHubIntegration: (options) => createGitHubIntegration({ ...options, fetch: github.fetch }),
    });
    assert.deepEqual(server.errors, []);
    assert.equal(result.status, "success");
    assert.match(await readFile(join(f.output, "response.md"), "utf8"), /\[REDACTED\] \[REDACTED\]/);
    const records = JSON.parse(await readFile(join(f.output, "github.json"), "utf8"));
    assert.equal(records[0].type, "proposal");
    assert.equal(github.requests.filter((r) => r.method === "POST").length, 0);
  } finally { await server.close(); await f.close(); }
});

test("provider errors, invalid configuration, and turn limits fail rather than accepting a prior response", async () => {
  for (const scenario of ["invalid-config", "turn-limit", "provider-error", "provider-length"]) {
    const f = await fixture(), github = githubFixture();
    const server = await modelServer(() => scenario === "provider-error" ? { status: 400 } : scenario === "provider-length" ? { delta: { content: "Incomplete response" }, finish: "length" } : tool("github_read_pr", {}, "repeat"));
    if (scenario === "invalid-config") f.config.profile = "missing";
    else if (scenario === "turn-limit") f.config.maxTurns = 1;
    try {
      await assert.rejects(runAgent(f.config, f.output, { configureModel: server.configureModel,
        createGitHubIntegration: (options) => createGitHubIntegration({ ...options, fetch: github.fetch }),
      }));
      const metadata = JSON.parse(await readFile(join(f.output, "run.json"), "utf8"));
      assert.equal(metadata.status, "failed");
      assert.equal(server.requests.length, scenario === "invalid-config" ? 0 : 1);
      await assert.rejects(readFile(join(f.output, "response.md")));
    } finally { await server.close(); await f.close(); }
  }
});

test("real SDK publication requires proposal receipt and records the exact inspected commit", async () => {
  const f = await fixture(), github = githubFixture();
  f.config.capabilities = "read,review-propose,review-publish";
  const server = await modelServer((request, count) => {
    if (count === 1) return tool("github_propose_review", { body: "Explicitly authorized comment." }, "propose");
    if (count === 2) {
      const message = request.messages.filter((message) => message.role === "tool").at(-1);
      return tool("github_publish_review", { proposal: JSON.parse(message.content).id }, "publish");
    }
    return { delta: { content: "Published with a receipt." } };
  });
  try {
    await runAgent(f.config, f.output, { configureModel: server.configureModel,
      createGitHubIntegration: (options) => createGitHubIntegration({ ...options, fetch: github.fetch }),
    });
    assert.deepEqual(server.errors, []);
    const records = JSON.parse(await readFile(join(f.output, "github.json"), "utf8"));
    assert.equal(records.at(-1).type, "published");
    assert.equal(records.at(-1).head, "a".repeat(40));
    assert.equal(github.requests.filter((r) => r.method === "POST").length, 1);
  } finally { await server.close(); await f.close(); }
});

test("whole-task deadlines fail closed and retain a failure artifact", async () => {
  const f = await fixture();
  f.config.capabilities = "";
  f.config.timeoutMs = 1;
  try {
    await assert.rejects(runAgent(f.config, f.output));
    assert.equal(JSON.parse(await readFile(join(f.output, "run.json"), "utf8")).status, "failed");
    await assert.rejects(readFile(join(f.output, "response.md")));
  } finally { await f.close(); }
});
