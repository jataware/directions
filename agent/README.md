# Directions Agent action

One task runner, powered by **Pi SDK 1.4.0** with an npm lockfile. Integration
capabilities are ordinary Pi extensions, not a second plugin framework.

Use `jataware/directions/agent@<full-sha>` after publishing this repository and
pinning a reviewed commit. No remote repository or release has been published
by this implementation.

## Minimal invocation

```yaml
- uses: jataware/directions/agent@REPLACE_WITH_FULL_COMMIT_SHA
  id: agent
  with:
    task: Inspect this PR and retain a review proposal. Do not publish.
    model: openai/gpt-6.1-sol
    api-key: ${{ secrets.OPENAI_API_KEY }}
    profile: review
    github-capabilities: read,review-propose
```

Check out a **trusted guidance branch** first, with checkout's
`persist-credentials: false`. The action does not check out or execute PR code.
The GitHub extension reads PR source through the API at the inspected commit.

See [consumer workflows](../examples/github/README.md) for complete triggers,
permissions, secrets, example profiles, artifacts, and supported operations.

## Inputs and results

See [action.yml](action.yml) for the complete input contract.

- Required: `task`, `api-key`.
- Model: optional exact Pi catalog `provider/model`, default `openai/gpt-6.1-sol`.
- Guidance: `workspace` (default GitHub workspace), `profile` (default `default`).
- Runtime: `thinking` (default `off`), `timeout-seconds` (default 600,
  maximum 1800), `max-turns` (default 16, maximum 100). Pi clamps thinking
  to model capabilities. No fallback/replay occurs. Identifier conventions and
  models.dev references are documented in [the spec](../SPEC.md#5-model-preferences-not-provider-management).
- Integrations: `extensions` (approved absolute local paths),
  `github-capabilities` (empty by default), `github-token` (default GitHub
  workflow token), `github-review-events` (default `COMMENT`), and
  `pull-request` for manual dispatch.

Outputs point to a private directory beneath `RUNNER_TEMP`:
`artifact-directory`, `response-file`, and `run-file`.

- `response.md`: final Markdown, only on successful model completion.
- `run.json`: status, failure stage, model/profile, active tools, warnings,
  turn/tool-call counts. Provider error payloads are intentionally not logged.
- `github.json`: proposals, attempted writes, and positive receipts when
  the bundled GitHub extension was enabled. Proposals are not receipts.

The action does not automatically upload files or place model text in workflow
commands/job summaries. Upload artifacts explicitly with suitable retention;
they can contain private repository content. Failure artifacts are useful even
if a publication succeeded and the subsequent model response failed.

## Execution boundaries

The action installs **its own** locked dependencies with scripts disabled and
builds the trusted Go client (Go >=1.24). It does not install consumer dependencies.

The SDK runs from a new empty working directory, with in-memory settings,
credential storage, and sessions. No user/project Pi settings, extensions,
skills, MCP servers, prompt templates, or system-prompt files are discovered
from the inspected checkout. The default tool set contains **no built-in
shell or filesystem tools**.

The Go client reads only the selected checkout root's Directions configuration
and one native context file (AGENTS.override.md, AGENTS.md, AGENTS.MD, CLAUDE.md,
or CLAUDE.MD, in that order). It does not load ancestor/home context in CI.
Prompt/override reads are bounded and traversal-confined using `os.Root`.
A malformed or missing selected profile fails before model execution. A checkout
without `.directions/` may still supply a root AGENTS file.

For PR tasks, diff policy matching uses the API's complete changed-path list,
including old rename paths—not the clean guidance checkout's local Git diff.
Issue planning uses an explicit empty diff. Every profile inherits all policies
unless a whitelist or blacklist restricts eligibility; only applicable policy
instructions are included. A conflicting pair of lists fails before the model runs.
Profile model/thinking preferences are guidance only; workflow runtime inputs
win. Provider/auth/endpoint fields are not part of Directions. Executable plugins
are loaded only from workflow-approved sources, not repository configuration.

Cancellation, a turn limit, failed extension handler, empty output, and any
non-`stop` final assistant reason fail the run. The action process has an
additional hard cleanup deadline for a misbehaving trusted extension. Known
supplied credentials are redacted from task/context/tool boundaries and artifacts.
This is defense in depth, not a guarantee that arbitrary private file content
contains no secrets.

`pull_request_target` is rejected. Read the GitHub extension's authorization
contract before enabling publication. A disabled publishing tool is meaningful
here because the default runner supplies no alternate shell/credential-reading
tools; an explicitly installed extension can change that capability surface.

## Load another ordinary Pi extension

Install the reviewed package and its dependencies separately, outside the
inspected workspace, using a pinned source and reviewed lockfile. Then:

```yaml
- uses: jataware/directions/agent@REPLACE_WITH_FULL_COMMIT_SHA
  with:
    task: Perform the requested integration task.
    model: openai/gpt-6.1-sol
    api-key: ${{ secrets.OPENAI_API_KEY }}
    extensions: |
      ${{ runner.temp }}/approved-pi-tools/node_modules/@example/pi-extension
```

Both extension files and ordinary Pi package directories with a `pi.extensions`
manifest work. Relative paths, missing paths, and paths resolving inside the
inspected checkout are rejected. The action does not fetch or install sources
listed in `extensions`; it loads only those explicit local resources. Pass
plugin configuration through its normal host-owned environment contract.

For example, stage a *reviewed, default-branch-owned* package.json/package-lock.json
into `RUNNER_TEMP/approved-pi-tools` and run `npm ci --ignore-scripts` there
before this step. Do not derive the package list, version, path, or credentials
from a PR body, changed config, or other untrusted input.

Extensions execute as trusted host code with the action process's permissions
and credentials. They are **not sandboxed** or made safe by hiding their tools.
Review their entire dependency closure. Directions policies cannot approve an
extension or grant capabilities. Service-specific integrations remain external.

## Current scope

Working: PR snapshot/pinned-source reads, issue reads and general discussion,
review/comment proposals, explicitly enabled publication, and externally
provided Pi extensions.

Not implemented: working-tree editing, shell/test execution, commits/pushes,
PR creation, PR-description replacement, issue-body replan replacement,
inline review findings/replies/resolution, and CI-log ingestion. Proposal-only
workflows must not be mistaken for code-editing or body-replacement workflows.
