# Guidance selection without owning the agent

Directions selects guidance; the host harness owns the agent, tools, model,
credentials, verification, retries, and publication. The existing Pi-backed
Action is an optional consumer, not a requirement for using the selector.

## Shared contract

The Go library's `BuildContext(ctx, root, ContextOptions)` and the Go CLI share
one selector. Inputs are a guidance snapshot, profile, host-selected operation,
and changed paths. Selection uses no model calls. Given identical source bytes
and inputs it is deterministic; local dirty files, unknown diffs, and mutable
host-approved HTTPS sources are not reproducible snapshots.

```sh
# Build once from a trusted Directions checkout.
(cd clients/cli && go build -o /trusted/bin/directions .)

# Local review: staged, unstaged, and nonignored untracked paths.
directions context /path/to/repository --operation review

# Explain includes exclusions, but no instruction bodies.
directions explain /path/to/repository --operation review

# CI or local branch review: host supplies the complete changed-path array.
# Both rename paths belong in this array; deletions do not need to exist.
directions context /trusted/guidance --exact-root --operation review \
  --diff-file /host/changed-paths.json

# Explicit scope expansion, e.g. an unchanged caller the reviewer should inspect.
directions explain /trusted/guidance --exact-root --operation review \
  --diff-file /host/changed-paths.json --path src/caller.go
```

Flags follow the directory argument. `--path` is repeatable and does not alter
the host diff. Paths are deduplicated and sorted. An empty host array means a
known empty diff, not a fallback to local Git. Additional paths cannot make an
unknown diff complete. Without a host diff, only local working changes are
examined: this does **not** compare a clean branch against its base.

`context` returns bounded model-facing `content`, `operation`, `diff_known`,
`decisions`, `warnings`, and optional host-requested root `agents` context.
Each decision includes a kind, ID, contributing sources, scope globs, inclusion
boolean, reason, and matching paths when applicable. Policy sources are all
contributing TOML documents in composition order, not field-level provenance.
`explain` runs the same loading and validation, but emits only diagnostics,
never instruction bodies or native context. It is not an offline/no-read mode:
explicit HTTPS approvals still apply. Inspect warnings as well as decisions.

## Central rules and directory-local review guidance

Keep `.directions/base.toml` as the entry point. Existing profiles, override
composition, glob matching, and whitelist/blacklist behavior remain unchanged.
A profile is a reusable set of guidance; an operation describes the current
activity. They are independent and neither enables capabilities.

```toml
[policies.security.api]
title = "Review authorization boundaries"
instructions = "Check authorization on every new endpoint."
operations = ["review"]
triggers.diff = ["services/**", "shared/auth/**"]
```

Omitting `operations` preserves applicability to all operations. A supplied list
must be nonempty and contain valid lower-case operation names. The default host
operation is `general`; it does not include review-only rules. Profiles still
control eligibility, then operation and diff triggers narrow applicability.
Cross-directory concerns belong in explicit centralized rules, not guessed
semantic dependencies.

For `--operation review`, the selector additionally reads `REVIEWER.md` at the
root and along ancestor directories of changed/additional paths. For example,
`src/api/handler.go` selects `REVIEWER.md`, `src/REVIEWER.md`, and
`src/api/REVIEWER.md` if present. Sibling directory files are not loaded. All
applicable files are additive, deduplicated, and ordered lexicographically;
a deeper file does not replace its ancestors. Conflicting guidance should be
resolved by maintainers rather than a hidden precedence mechanism.

Review files are independent of profile policy lists. An empty whitelist
selects no TOML policies; it does not disable review files or the profile prompt.
Choose another operation if directory review guidance is unwanted. No
`AGENTS.md` discovery/inheritance system is added: native harness instructions
remain the harness's responsibility. The Action's existing opt-in root native
context support is unchanged.

Discovery only checks relevant ancestors, not an entire repository. Nested Git
roots and symlink directories are boundaries. Reads use confined `os.Root`
access; escaping file symlinks and nonregular files fail. Missing ancestor
directories are allowed for deleted paths. Limits: 6000 total input paths,
4096 visited directories, 32 review files, 64 KiB per review file and 64 KiB
assembled Directions content. Policy/reviewer diagnostics are capped at 512 KiB.
Exceeding a limit fails rather than silently
truncating coverage.

With an unknown diff, eligible triggered policies remain conservatively
included. Review-file discovery can only include root guidance and explicit
additional scope; a warning reports incomplete directory coverage. Never
interpret unknown scope as a complete review.

## Thin integrations

The Pi extension refreshes through `directions context` before each prompt.
Use host flags `--directions-profile review --directions-operation review` to
select review guidance; it injects the returned content without starting a
second agent. Native `AGENTS.md` loading and runtime settings are unchanged.

The optional Action accepts independent `profile` and `operation` inputs and
records selection diagnostics in `run.json`. Its GitHub adapter supplies pinned
PR changed paths (including renames) separately from the trusted guidance
checkout. Reviewer workflow examples explicitly set `operation: review`.

Other harness hooks can call the same CLI and translate `content` into their
native context-injection format. A hook should check the exit status, surface
warnings/failures, clear stale guidance on failure, and never interpret returned
instructions as shell commands. No new hook protocol or generic runner is
required. Native integrations beyond Pi are not implemented here.

## Trust and future scope

In CI, **the host must select a trusted base/default-branch guidance checkout**
and obtain changed paths separately from a pinned PR snapshot. `--exact-root`
prevents ancestor discovery; it does not verify checkout provenance. The
selector deliberately does not fetch/check out revisions or decide which branch
is trusted. Do not point it at PR-modified instructions when running privileged
review automation. Keep executables, hook configuration, and permissions under
host control too. Guidance cannot approve URLs or grant tools or credentials.

A diff is an explicit scope heuristic, not semantic dependency analysis.
`--path` allows intentional expansion; no claim is made to find every impacted
unchanged caller. Future implementation, description, and triage operations can
reuse the operation filter and selector. Their lifecycle, mutation permissions,
verification gates, and publication remain host concerns, not selector features.
