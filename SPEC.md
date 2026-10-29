# Directions format — v1.0.0 (draft)

Directions is a repository-local convention for guidance consumed by humans
and tools. It is not tied to a forge, agent, or model provider.

## Values

- **Documented rules:** make expectations explicit, inspectable, and versioned
  alongside the repository.
- **Repeatable agent behavior:** consistent guidance and deterministic selection
  make behavior easier to reproduce and evaluate. This does not promise
  deterministic model outputs.
- **Minimal context, on demand:** only provide what is needed, **as it is
  needed**, on the fly. Discover relevant guidance and apply its triggers rather
  than dumping every eligible policy into every request.

Policy eligibility is not unconditional context inclusion. Consumers SHOULD
refresh relevant guidance as the task scope changes and MUST document their
refresh granularity. The desktop Pi adapter currently refreshes before each user
prompt; the action assembles one scoped context per run and exposes source and
discussion tools on demand. Mid-turn, tool-triggered policy loading is not yet
implemented. Unknown diff applicability currently uses bounded conservative
inclusion with an explicit warning rather than silently omitting rules.

The key words MUST, SHOULD, and MAY describe the proposed format. The Go
implementation provides single-document parsing and structural validation plus
bounded context assembly with local overrides, prompt reads, and Git diff policy
matching. The Pi adapter consumes that context without configuring providers.

## 1. Discovery and version

The entry point is `.directions/base.toml` under an explicitly selected
repository root. A consumer MUST NOT silently walk into a different repository
or load user-global configuration. Automatic discovery, if supported, must be
documented by that consumer.

`meta.version = "v1.0.0"` is a quoted TOML string describing the format version,
not the repository release or client version. Clients MUST reject unsupported
versions. Duplicate TOML tables and unknown keys are errors, not silently
ignored typos. Empty collections and absent optional fields are allowed.

The base document MUST declare `meta.version` and `repo.vcs`.
`repo.vcs` is `"git"` or `"none"`; neither value runs a command.

Prompt and override references MUST be nonempty strings: either local paths or
absolute `https://` URLs. Local paths are relative to the selected project root
(the directory containing `.directions/`), even in fragments. Use forward slashes;
absolute filesystem paths, escaping the root via `..`, backslashes, tilde prefixes,
NUL, and other URI schemes are unsupported. No variable or tilde expansion occurs.
Parsing references does not read files, fetch URLs, or authorize access.
Profile, group, and policy names use lower-case ASCII letters, digits,
underscores, and hyphens.

## 2. Profiles

`[profiles.<name>]` describes reusable task guidance:

- `prompt`: optional project-root-relative path or HTTPS URL, such as
  `.directions/contexts/general.md` or `https://example.org/guidance.md`.
- `model`: optional qualified `provider/model` preference, such as
  `openai/gpt-6.1-sol`. Provider connection settings are not part of Directions.
- `thinking`: optional nonempty, adapter-specific hint. Consumers MUST report
  unsupported values rather than quietly pretending to honor them.
- `whitelist` or `blacklist`: optional arrays of policy selectors, mutually
  exclusive by **presence**, even if one or both arrays are empty.

A profile without model/thinking preferences is valid for non-agentic consumers.
A model selection is not proof that credentials or runtime support exist.
Generic parsing does not select a profile. An adapter MUST document its choice;
the desktop Pi extension selects `profiles.default`. CLI `context --profile NAME`
and the Agent action select the explicitly requested profile (default `default`).

### Default: all policies eligible

```toml
[profiles.default]
prompt = ".directions/contexts/general.md"
```

With neither selection list present, **every policy in every group** is eligible,
including policies introduced by override files. Triggers are then evaluated to
decide which policy instructions belong in the current context.

### Whitelist: only matching policies eligible

```toml
[profiles.default]
prompt = ".directions/contexts/general.md"

[[profiles.default.whitelist]]
group = "meta"
# policy = "only-prompts-in-contexts-dir"
```

Omitting `policy` selects the whole named group; specifying it selects one policy.
A present whitelist switches the profile to whitelist mode. Explicit
`whitelist = []` selects **no policies**, while retaining the profile prompt.

### Blacklist: all policies except matches eligible

```toml
[profiles.default]
prompt = ".directions/contexts/general.md"

[[profiles.default.blacklist]]
group = "meta"
# policy = "only-prompts-in-contexts-dir"
```

The same group/policy selector shape excludes the whole group or one policy.
Explicit `blacklist = []` excludes nothing, so all policies remain eligible.

Unknown group/policy references are errors in **either** mode after composition.
Duplicate selectors are harmless; a policy is included at most once, in stable
group/policy order. A single-document parse cannot establish references that may
come from overrides.

Both lists MUST NOT coexist, including after overrides are merged. Overrides
replace arrays of the same name; they do not erase a differently named list.
To change between whitelist and blacklist, remove the previous mode's declaration
rather than leaving contradictory configuration. The plural `profiles` table is
canonical; singular `profile` and the old profile `policies` selector key are errors.

## 3. Policies

Policies are grouped as `[policies.<group>.<policy>]`.
Each policy requires nonempty `title` and `instructions` strings.

`default-impact` is optional and, when present, one of `low`, `medium`,
`high`, or `blocking`. It expresses finding severity, not authorization.
Consumers define and document how severity affects their behavior.

`triggers.diff` is an optional nonempty array of repository-relative glob
patterns. With no trigger, a selected policy always applies. With triggers,
at least one changed path must match. When diff data is unavailable, a consumer
MUST report triggered-policy applicability as unknown, not silently skip it.
Paths use forward slashes. Globs support `*`, `?`, character classes, and
`**` as a whole path segment matching zero or more segments. Matching is
case-sensitive and against the entire repository-relative path.

The Go parser validates trigger shape; the context assembler validates and
matches globs when selecting policies.
Directions instructions are data: they MUST NOT authorize code execution,
credential access, or publication.

## 4. Overrides (proposed composition)

`[[overrides]]` contains one required string `path`: a project-root-relative TOML
file/glob or an HTTPS URL to one TOML fragment. For example:

```toml
[[overrides]]
path = ".directions/policies/*.toml"

[[overrides]]
path = "https://example.org/directions/security.toml"
```

A composition loader MUST:

1. Read the base document, then expand override entries in declaration order.
2. Sort matches within an entry lexicographically by normalized path. Zero
   matches are allowed for globs; a missing literal file is an error.
3. Load fragments using the same field schema; fragments may omit `meta` and
   `repo`, and may partially override tables (for example just a policy's
   instructions). Required values are checked after merging. A version, if
   supplied, MUST be supported and equal to the base.
4. Recursively merge tables; later scalar values and arrays replace earlier
   values in full. No implicit array concatenation or deletion syntax exists.
5. Reject attempts to override `meta.version` or `repo.vcs` with different
   values. Local paths remain project-root-relative when fragments move.
6. Reject nested override declarations in fragments for this version.
7. Validate references against the composed result before running a task.

Local file reads MUST be confined to regular files under the selected
project root, including symlink target checks. Consumers MUST cap file counts,
sizes, and match expansion. Lexical path validation alone does not establish
confinement.

`Parse`/`ParseFragment` and CLI `validate` inspect single documents; they do not
compose overrides. `InitialContext` and CLI `context` implement composition for
context assembly and enforce 32 override files, 1 MiB per file, 4 MiB total
config, and 4096 visited entries per glob. Glob traversal does not follow symlink
directories. The old path objects are rejected.

HTTPS loading requires host approval of each **exact reference**, including any
fragment. CLI `context --allow-url URL` is repeatable; the Go API uses
`ContextOptions.AllowedURLs`, Pi uses `--directions-allow-urls` (whitespace-separated),
and the action uses the workflow-owned `allowed-urls` input. None is enabled by
repository configuration. Unapproved sources fail without making a request.

URLs MUST fit in 4096 bytes and MUST NOT contain credentials, query strings,
literal whitespace, or glob patterns. Optional `#sha256=<64 hex digits>` verifies
the downloaded bytes before use; the fragment is not transmitted. Other fragments are rejected. Unpinned
sources emit a mutability warning; prefer a digest and/or an immutable URL.

The loader uses verified TLS, rejects redirects and non-200 responses, and sends
no cookies or authorization headers. Its default transport ignores environment
proxies and caps response headers at 16 KiB. Remote reads share a five-second
network budget per context build and the existing file/context/config limits.
There is only a per-build memory cache, not a persistent cache or stale fallback.
Host-approved destinations may include private services; URL approval is a trust
and network-access decision, not an assertion that a destination is public.

Remote TOML fragments MUST be self-contained: no `prompt` references or nested
`overrides`, even when the referenced targets are separately approved. Remote
prompt text is data, not a list of resources to fetch. No relative URL resolution
or implicit follow-on local/network reads occur.

## 5. Model preferences, not provider management

Profiles MAY include optional runtime preferences:

```toml
[profiles.default]
model = "openai/gpt-6.1-sol"
thinking = "medium"
```

`model` is a nonempty `provider/model` reference. The provider identifier follows
the same name rules as profile names; the model ID is the opaque remainder after
the first slash and may itself contain slashes. Whitespace/control characters
are invalid. Parsing validates the reference shape, not model availability.
Omit these fields entirely for non-AI consumers or to keep the client's runtime.

### Identifier reference: models.dev

Directions follows the `provider/model` identifier convention documented by
[models.dev](https://models.dev/), an open model/provider catalog:

- [Catalog API](https://models.dev/api.json): entries are keyed by provider ID,
  with each provider's models keyed by provider-scoped model ID.
- [Model identifier schema](https://models.dev/model-schema.json): a JSON Schema
  enumerating known `provider/model` references.

Split on the **first** slash: the prefix identifies the inference provider/API
host, not necessarily the organization that created the model. The remainder
is that provider's model ID, which may contain additional slashes. Do not replace
it with a display name, insert spaces, or assume IDs are interchangeable across
providers.

models.dev is a reference catalog, not a universal identifier authority or a
closed allowlist. Custom/private provider/model references remain valid if they
satisfy Directions syntax and the consuming runtime can resolve them. Parsing
does not fetch models.dev or import provider settings. A catalog listing alone
does not establish runtime support, access, or credentials; the action validates
availability against its pinned Pi catalog.

The repository's starter configuration, examples, and Agent action default to
`openai/gpt-6.1-sol` (GPT-6.1 Sol). This is a project default, not a mandatory
model for every Directions consumer. The action may override it through its
`model` input; omitted profile preferences still leave the host runtime unchanged.

Directions does **not** define provider catalogs, supported-model lists, API
types, endpoints, credential discovery, OAuth flows, or credential-file paths.
Those belong to the consuming client's native configuration or trusted workflow
environment. The `providers` table and separate profile `provider` field are
unsupported and MUST be rejected rather than silently ignored.

A preference is not authorization to register providers, redirect requests,
access credentials, or override host restrictions. Consumers MUST document how
they interpret preferences and report when they do not apply them. Current
context-only adapters leave the host's model/thinking unchanged and emit a short
notice without injecting the requested model name into model context. The Agent
action uses explicit workflow model/credential inputs instead.

## 6. Trust and integration boundaries

Contexts are reusable prompt/guidance content, not executable configuration.
Repository guidance may be untrusted, including changes from pull requests.
It cannot override host permission checks, approve its own provider endpoints,
or enable plugins, shells, external downloads, or secret access.

Active GitHub workflows remain under `.github/workflows/`.
Pi and OpenCode extensions remain installed and authorized by their hosts.
The initial Pi extension adds guidance through a structured system-prompt section,
leaving native `AGENTS.md` loading and host runtime settings intact. Selected
policies match staged, unstaged, and nonignored untracked local paths, including
both rename paths; no remote or branch comparison occurs. Missing diff data causes
conservative policy inclusion with an explicit unknown-applicability note.

The Agent action uses exact-root discovery, optional root AGENTS context, and
host-supplied changed-path arrays for PR tasks. These inputs are trusted host
configuration, not new repository permissions. The reusable GitHub Pi extension
implements scoped reading, proposal validation, and explicitly authorized
publication separately from the runner. External extensions use Pi's normal
package format and must be installed/approved by the workflow.

MCP exposure, fallback behavior, code mutation, and configuration-format adapters
remain separate work. See `agent/README.md` and `tools/pi/github/README.md` for
the action's capability and publication contracts.
