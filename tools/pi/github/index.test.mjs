import assert from "node:assert/strict";
import test from "node:test";
import { createGitHubIntegration, parseCapabilities, targetFromEvent, targetKindFromEvent } from "./index.mjs";
import { githubFixture, HEAD, SOURCE } from "../../../agent/fixtures/github.mjs";

async function tools(fixture, overrides = {}) {
  const integration = await createGitHubIntegration({ ...fixture.options, ...overrides });
  const registered = new Map();
  integration.extension({ registerTool(tool) { registered.set(tool.name, tool); }, on() {} });
  return { integration, names: [...registered.keys()], call: async (name, params = {}) => {
    assert.ok(registered.has(name), name + " not registered");
    const value = await registered.get(name).execute("call", params, new AbortController().signal);
    return JSON.parse(value.content[0].text);
  } };
}

test("capabilities are explicit and target cannot be redirected by an event", () => {
  assert.deepEqual([...parseCapabilities("")], []);
  for (const caps of ["all", "review-publish", "comment-publish", "review-propose", "comment-propose"]) assert.throws(() => parseCapabilities(caps));
  assert.equal(targetFromEvent({ pull_request: { number: 7 } }, "pull_request"), 7);
  assert.equal(targetFromEvent({ issue: { number: 7 } }, "issue_comment"), 7);
  assert.equal(targetKindFromEvent({ issue: { number: 7 } }, "issue_comment"), "issue");
  assert.throws(() => targetFromEvent({ pull_request: { number: 7 } }, "pull_request", 8));
  assert.throws(() => targetFromEvent({}, "pull_request_target", 7));
});

test("read-only mode pins file reads and does not expose publication or proposal tools", async () => {
  const f = githubFixture();
  const t = await tools(f, { capabilities: "read" });
  assert.deepEqual(t.names.sort(), ["github_read_discussion", "github_read_file", "github_read_pr"]);
  const context = await t.call("github_read_pr");
  assert.equal(context.head, HEAD);
  assert.equal((await t.call("github_read_file", { path: "src/a.ts" })).content, SOURCE);
  assert.ok(f.requests.at(-1).url.includes("ref=" + HEAD));
  for (const path of ["../secret", "/secret", "a/../secret", "a//b", "a\0b"]) await assert.rejects(t.call("github_read_file", { path }));
  assert.ok(f.requests.every((request) => request.method === "GET"));
});

test("pagination is complete, includes rename paths, and surfaces missing patches", async () => {
  const files = Array.from({ length: 101 }, (_, i) => ({ filename: "src/" + i + ".ts", status: "modified" }));
  files[0].previous_filename = "old.ts";
  const f = githubFixture({ files });
  const t = await tools(f);
  assert.equal((await t.call("github_read_pr")).files.length, 101);
  assert.equal((await t.call("github_read_pr")).files[0].patch, null);
  assert.ok(t.integration.changedPaths.includes("old.ts"));
  assert.equal(f.requests.filter((r) => r.route.endsWith("/files")).length, 2);
});

test("validated review proposals do not publish; concurrent publication attempts permit at most one POST", async () => {
  const f = githubFixture();
  const t = await tools(f, { capabilities: "read,review-propose,review-publish" });
  const proposal = await t.call("github_propose_review", { body: "Focused review", event: "COMMENT" });
  assert.equal(proposal.published, false);
  assert.equal(f.requests.filter((r) => r.method === "POST").length, 0);
  const outcomes = await Promise.allSettled([
    t.call("github_publish_review", { proposal: proposal.id }),
    t.call("github_publish_review", { proposal: proposal.id }),
  ]);
  assert.equal(outcomes.filter((r) => r.status === "fulfilled").length, 1);
  const posts = f.requests.filter((r) => r.method === "POST");
  assert.equal(posts.length, 1);
  assert.equal(posts[0].body.commit_id, HEAD);
  assert.match(posts[0].body.body, /directions:review/);
  assert.equal(t.integration.records().at(-1).type, "published");
});

test("publication refuses stale heads/bases, closed and fork PRs, unknown proposals, and revoked permissions", async () => {
  for (const change of [{ head: "c".repeat(40) }, { base: "c".repeat(40) }, { prState: "closed" }, { repository: "fork/repo" }, { permission: "read" }]) {
    const f = githubFixture();
    const t = await tools(f, { capabilities: "read,review-propose,review-publish" });
    await assert.rejects(t.call("github_publish_review", { proposal: "not-a-proposal" }));
    const proposal = await t.call("github_propose_review", { body: "Proposed review" });
    Object.assign(f.state, change);
    await assert.rejects(t.call("github_publish_review", { proposal: proposal.id }));
    assert.equal(f.requests.filter((r) => r.method === "POST").length, 0);
  }
  const f = githubFixture({ permission: "read" });
  await assert.rejects(tools(f, { capabilities: "read,review-propose,review-publish" }));
  await assert.rejects(tools(f, { eventName: "issue_comment", capabilities: "read" }));
});

test("ambiguous publication failure is recorded and never retried", async () => {
  const f = githubFixture({ failWrite: true });
  const t = await tools(f, { capabilities: "read,review-propose,review-publish" });
  const proposal = await t.call("github_propose_review", { body: "Review" });
  await assert.rejects(t.call("github_publish_review", { proposal: proposal.id }));
  await assert.rejects(t.call("github_publish_review", { proposal: proposal.id }));
  assert.equal(f.requests.filter((r) => r.method === "POST").length, 1);
  assert.equal(t.integration.records().at(-1).type, "publication-attempt");
});

test("issues support pinned source, paged discussion, and explicit comment proposals/publication", async () => {
  const f = githubFixture();
  const t = await tools(f, { kind: "issue", eventName: "issue_comment", capabilities: "read,comment-propose,comment-publish" });
  assert.ok(!t.names.includes("github_read_pr"));
  assert.ok(!t.names.includes("github_publish_review"));
  assert.equal((await t.call("github_read_issue")).sourceCommit, HEAD);
  assert.equal((await t.call("github_read_discussion")).comments[0].body, "Latest requirement.");
  const proposal = await t.call("github_propose_comment", { body: "Implementation plan." });
  assert.equal(proposal.published, false);
  await t.call("github_publish_comment", { proposal: proposal.id });
  assert.equal(f.requests.filter((r) => r.method === "POST")[0].route, "/repos/owner/repo/issues/7/comments");
});

test("proposal types cannot cross publication tools, and malformed configuration fails early", async () => {
  const f = githubFixture();
  const t = await tools(f, { capabilities: "read,review-propose,review-publish,comment-propose,comment-publish" });
  const comment = await t.call("github_propose_comment", { body: "Comment" });
  const review = await t.call("github_propose_review", { body: "Review" });
  await assert.rejects(t.call("github_publish_review", { proposal: comment.id }));
  await assert.rejects(t.call("github_publish_comment", { proposal: review.id }));
  await assert.rejects(t.call("github_propose_review", { body: " ", event: "COMMENT" }));
  await assert.rejects(t.call("github_propose_review", { body: "Review", event: "MERGE" }));
  for (const change of [{ repository: "../secret" }, { apiUrl: "http://evil.invalid" }, { eventName: "pull_request_target" }, { token: "" }, { kind: "issue", capabilities: "read,review-propose" }]) await assert.rejects(tools(f, change));
});
