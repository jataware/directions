# Directions for Pi

Minimal context-only Pi extension. Requires Pi's structured
`before_agent_start.systemPromptOptions.sections` API (tested with Pi 1.4.0)
and a separately built, trusted Directions executable. There are no npm runtime
dependencies and no Go compilation, downloads, or installs during Pi startup.

## Try it

From the Directions project directory:

```sh
(cd clients/cli && go build -o /absolute/trusted/path/directions .)

# Start normally in the repository you want to work on:
pi -e /absolute/path/to/directions/clients/agents/plugins/pi \
  --directions-bin /absolute/trusted/path/directions
```

`--directions-bin` defaults to `directions` on the host PATH. Prefer an absolute
path, especially if your PATH includes the working directory. This executable
must be installed by you, not supplied by the repository being inspected.

For automatic loading on normal starts, add this package to the appropriate
Pi configuration. For example, in the consuming repository:

```sh
pi install --local /absolute/path/to/directions/clients/agents/plugins/pi
```

That command modifies the consuming repository's `.pi/settings.json`; review
and preserve existing settings. Local packages are loaded in place, not copied.
Put the built CLI on your trusted PATH, or supply `--directions-bin` on startup.
The extension does not install itself or modify any host settings. Review the
extension/package before granting project trust.

## Behavior

- Discover the nearest `.directions/` from Pi's working directory upward,
  stopping at a Git boundary, including worktrees and submodules.
- No Directions entry: do nothing. An invalid entry/config is reported, not
  silently skipped in favor of a parent.
- Read `base.toml`, compose repository-local override files, select
  `profiles.default`, and read its prompt. Prompt and override paths are plain
  project-root-relative strings or HTTPS URLs. Approve remote references with
  `--directions-allow-urls "https://example.org/guidance.md"`; multiple exact
  references are whitespace-separated. No URL is implicitly authorized.
- Add a distinct `directions` system-prompt section. Pi continues to load
  `AGENTS.md`, ancestor instructions, and overrides normally; the extension
  does not load them a second time, replace the whole system prompt, or
  bypass `--no-context-files` for native files.
- All policies are eligible by default. `profiles.default.whitelist` selects
  only matching policies; `profiles.default.blacklist` excludes matches. They
  cannot coexist, including empty arrays. An empty whitelist selects none.
  No trigger means an eligible policy is always included. Diff triggers match
  staged changes, unstaged changes, and
  untracked nonignored filenames. Both sides of renames and deletions count.
  Paths are relative to the selected Directions root, even when it is nested
  inside a Git repository. Unborn repositories are supported.
- No branch/merge-base/remote comparison is made; committed changes with a
  clean working tree do not trigger a policy.
- If Git is unavailable/fails, or `repo.vcs = "none"`, include selected
  diff-triggered policies conservatively and mark applicability unknown.
- Refresh on session startup and before every prompt. This picks up new diffs,
  changed guidance, `/new`, `/resume`, `/reload`, and context compaction
  without appending duplicate conversation messages.
- Missing profiles/references, malformed config, unsafe paths, or oversized
  context clear the previous section and replace it with a visible load-error
  note. Interactive warnings are deduplicated; headless runs get notes in
  model context, never ad hoc stdout. Errors do not block the user's prompt.

## Deliberate limits

This adapter does not change models or thinking levels, manage providers,
execute instructions, or enforce policy severity. Profile model/thinking
preferences produce a short context-only notice; Pi keeps its native runtime
configuration, including providers, endpoints, and credentials.

Local files must be regular files confined to the selected project root using
Go's traversal-resistant `os.Root`. Absolute filesystem paths and old path objects
are rejected. HTTPS sources require exact host approval (including digest
fragments), verified TLS, bounded reads, and no redirects or ambient credentials.
Use `#sha256=<64 hex digits>` to pin bytes; unpinned sources warn and may change.
Remote fragments cannot introduce prompts or further overrides. Override globs
do not traverse symlink directories. Provider/credential configuration is not part of Directions and is
rejected by the parser. This is not a sandbox; repository guidance remains
untrusted input to the model.

Limits: 1 MiB per config file, 4 MiB total configuration, 32 override files,
4096 visited entries per override glob, and 64 KiB rendered context. Git name
output is capped at 1 MiB per command; each Git command has a two-second timeout,
and the extension imposes a ten-second CLI timeout. Git external diffs, text
conversion, clean/process filters, fsmonitor, lazy fetching, and inherited
Git-routing environment variables are disabled for inspection.

## Tests

Requires Go >=1.24 and Node >=22.18 (native TypeScript type stripping):

```sh
npm test
```

Tests exercise lifecycle hooks with a fake host and an end-to-end CLI/bootstrap/
Git fixture without model calls or credentials. Go library tests cover config
composition, globs, dirty paths, symlink confinement, and discovery boundaries.
