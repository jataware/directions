# Directions

Repository-owned guidance for people, agents, and automation.

Directions values **documented rules**, **repeatable agent behavior**, and
**minimal context: only what is needed, as it is needed—on the fly**.

**Status: draft.** Directions is a standalone repository, intended for
`jataware/directions`. The configuration format version is `v1.0.0`; this is not
a claim that the clients are complete or that a release has been published.

## Layout

```text
README.md
SPEC.md
clients/
  lib/
    go/             # parser, context assembly, policy/diff matching, safe paths
    python/         # reserved; future Pydantic AI integration
    ts/             # reserved
  cli/              # Go CLI using clients/lib/go
  agents/
    mcp/            # reserved; will use clients/lib/go
    plugins/
      pi/           # context-only Pi extension
      opencode/     # reserved
agent/              # single Pi-backed GitHub Action
tools/
  pi/
    github/         # reusable, capability-scoped GitHub Pi extension
examples/github/    # consumer workflows and example Directions profiles
.github/            # contribution templates only; no active workflows
.directions/
  base.toml
  contexts/general.md
bootstrap.sh
LICENSE
```

The proposed consumer directory is `.directions/`. It is not an execution
directory: GitHub workflow files still belong in `.github/workflows/`.
Policies describe constraints; only host code can enforce permissions.

## Bootstrap

From a local checkout of this project:

```sh
./bootstrap.sh /path/to/repository
# Omit the argument to initialize the current directory.
```

The script copies the starter base configuration and general context, creates
an empty policies directory, and refuses to replace an existing
`.directions/` entry (including symlinks). It does not install software,
read credentials, execute repository code, or modify GitHub workflows.
Review the example model preference and configure your client separately.
Prompt and override paths are plain strings relative to the selected project
root, such as `.directions/contexts/general.md`—never relative to the TOML file.
Absolute paths, URLs, and the old path objects are unsupported.

## Go CLI

Requires Go 1.24 or newer (traversal-resistant `os.Root` file access). No Go
workspace setup is needed.

```sh
cd clients/cli
go run . validate ../../.directions/base.toml
go run . context ../.. # JSON guidance for the nearest Directions root
go test ./...
cd ../lib/go
go test ./...
```

`validate` parses and structurally validates **one document**. It does not
expand overrides, resolve profile/policy references across documents, read
prompt files, authenticate, or execute instructions. Successful validation is
not evidence that a provider/model exists or that an integration is usable.

`context WORKING_DIRECTORY` discovers the nearest Directions root (stopping at
Git boundaries), composes local overrides, loads the default prompt, and selects
policies using the local staged/unstaged/untracked diff. It returns JSON for
adapters without runtime preferences or credentials in model context. Optional host flags:
`--profile NAME`, `--diff-file JSON` (array of changed paths), `--agents` (one
root context file), and `--exact-root` (no ancestor discovery). The action uses
these flags so a trusted guidance checkout and a pinned PR diff remain separate.

## Model preferences

Profiles may specify `model = "openai/gpt-6.1-sol"` and `thinking = "medium"`, or
omit them entirely. Directions does not manage provider catalogs, endpoints,
API types, OAuth, or credentials; use the client or workflow configuration.
The current context adapters leave host runtime choices unchanged. Starter
examples and the Agent action default to `openai/gpt-6.1-sol`; see the
[models.dev identifier references](SPEC.md#identifier-reference-modelsdev).

## Policy selection

Use `[profiles.<name>]`. Every profile includes all policies by default, with
triggers determining which instructions are relevant to the current context.
`[[profiles.default.whitelist]]` restricts eligibility to matching group/policy
selectors; `[[profiles.default.blacklist]]` excludes matches instead. The two
lists cannot coexist, even across overrides. Explicit `whitelist = []` selects
none; `blacklist = []` keeps all. See [the spec](SPEC.md#2-profiles) for examples.

## Pi

The [Pi extension](clients/agents/plugins/pi/README.md) adds the default profile
and applicable selected policies alongside Pi's native `AGENTS.md` context. It
refreshes before each prompt and does not change models or access credentials.
See its README for build/install instructions and explicit safety limits.

See [SPEC.md](SPEC.md) for the proposed format and composition rules.
The Python, TypeScript, MCP, and OpenCode directories remain integration placeholders.

## GitHub Actions

The [Agent action](agent/README.md) runs one task through pinned Pi with explicitly
approved ordinary extensions. It starts without general shell/filesystem tools.
The [GitHub extension](tools/pi/github/README.md) supplies scoped PR/issue reads,
proposals, and separately enabled publication. Service-specific integrations remain
external.

Start with [examples/github](examples/github/README.md) for a reviewer, proposal-only
review, issue planner, PR-description proposal, and follow-up change plan. These
cover supported operations and clearly distinguish publication from proposals.

## Checks

From this directory, with Node >=22.19 and Go >=1.24:

```sh
npm ci --ignore-scripts --no-audit --no-fund
bash scripts/check.sh
```

The suite exercises real pinned-SDK calls against a loopback model fixture, with
fake GitHub transport/credentials. It makes no real model calls or GitHub writes.
This repository has **no active GitHub workflows**. CI is an inert example at
`examples/github/ci.yml`; nothing runs on push, pull requests, or manual dispatch
unless a workflow is explicitly copied into `.github/workflows/`.

## License

MIT; see [LICENSE](LICENSE).
