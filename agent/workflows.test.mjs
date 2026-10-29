import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { parseDocument } from "yaml";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
function yaml(path) {
  const document = parseDocument(readFileSync(resolve(root, path), "utf8"), { uniqueKeys: true });
  assert.deepEqual(document.errors, [], path);
  return document.toJS();
}
function steps(value, visit) {
  if (Array.isArray(value)) for (const item of value) steps(item, visit);
  else if (value && typeof value === "object") {
    visit(value);
    for (const child of Object.values(value)) steps(child, visit);
  }
}

test("repository has no active GitHub workflows", () => {
  const directory = resolve(root, ".github/workflows");
  assert.ok(!existsSync(directory) || !readdirSync(directory).some((name) => /\.ya?ml$/i.test(name)), "Keep workflows as examples until explicitly enabled");
});

test("action and workflow examples parse; executable action dependencies are pinned and shell input is not interpolated", () => {
  const action = yaml("agent/action.yml");
  assert.equal(action.runs.using, "composite");
  assert.equal(action.inputs["github-review-events"].default, "COMMENT");
  assert.equal(action.inputs.model.default, "openai/gpt-6.1-sol");
  assert.equal(action.inputs.model.required, false);
  assert.equal(action.inputs["allowed-urls"].required, false);
  const paths = ["agent/action.yml",
    ...readdirSync(resolve(root, "examples/github")).filter((name) => name.endsWith(".yml")).map((name) => "examples/github/" + name)];
  for (const path of paths) {
    const data = yaml(path);
    steps(data, (node) => {
      if (node.uses) assert.match(node.uses, /^\.\/|@(?:[a-f0-9]{40}|REPLACE_WITH_FULL_COMMIT_SHA)$/, path + ": " + node.uses);
      if (node.run) assert.ok(!node.run.includes("${{"), path + ": expressions must enter shell through env");
    });
    if (data.on) {
      assert.ok(!("pull_request_target" in data.on));
      assert.equal(data.permissions.contents, "read");
    }
  }
});
