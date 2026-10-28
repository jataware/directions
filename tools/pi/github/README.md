# GitHub tools for Pi

An ordinary Pi extension for narrowly scoped repository maintenance. The Agent
action bundles this extension but does not own its tool implementations.

## Capabilities

The host supplies a comma-separated set; none is enabled implicitly.

| Capability | Tools added |
| --- | --- |
| `read` | `github_read_pr` or `github_read_issue`, `github_read_file`, `github_read_discussion` |
| `review-propose` | `github_propose_review` (requires read and a PR target) |
| `review-publish` | `github_publish_review` (requires review-propose) |
| `comment-propose` | `github_propose_comment` (requires read) |
| `comment-publish` | `github_publish_comment` (requires comment-propose) |

Review outcomes are separately allowlisted by the host: `COMMENT` is the
default. `APPROVE` and `REQUEST_CHANGES` require explicit
`github-review-events`/ `reviewEvents` opt-in. Policy severity does not
automatically grant approval/request-changes authority.

Proposals are validated and retained in host memory, bound to the inspected
commit, and identified by a content digest. Publishing accepts only an ID from
this run, not arbitrary replacement JSON. Comment and review proposals cannot
be interchanged. There is at most **one publication attempt per run**, including
concurrent tool calls. Ambiguous network/API failures consume that attempt;
inspect GitHub before retrying in a new run. There is no automatic write retry.

## Scope and authorization

The repository and target number come from the trusted workflow event. An
explicit PR number is accepted for workflow_dispatch; other events cannot
redirect the target. Supported events: pull_request, issues, issue_comment,
pull_request_review_comment, workflow_dispatch. pull_request_target is rejected.

Comment-triggered runs and issues events require the workflow actor to have
write/maintain/admin repository permission **before model execution**, even
for read-only tools. Every publication additionally checks that permission
again. The actor is host metadata (the triggering actor on reruns), not a
username supplied by the model or inferred from token ownership.

Review publication requires an open, same-repository PR with unchanged head
**and base**. The POST includes the inspected commit_id and a host marker.
These are read-before-write checks, **not atomic compare-and-swap**: a head/base
may change between the final GET and POST. A receipt confirms GitHub accepted
a review for the inspected commit, not that the current head is still reviewed
or that branch protections are satisfied.

Comments append to the bound issue/PR; they never replace existing human text.
Issue source reads use the default branch's inspected commit, not a moving ref.
PR source reads use the inspected head repository/SHA, including read-only fork
snapshots. General discussion is explicitly paginated; inline PR review threads
are not yet available. Missing/binary/oversized patches are reported, not
presented as proof of clean code. File reads accept regular UTF-8 text under
512 KiB. The initial PR file list is complete or preparation fails (3000-file
limit); accumulated patches are capped at 1 MiB and each HTTP response at 2 MiB.

Only HTTPS, a fixed host-owned API base, scoped repository routes, bounded
responses, and no redirects are used. Request errors never include API bodies.
The runtime token is held in host closures, not model context.

## Use outside the action

From a source checkout, load this directory as a normal Pi package after
reviewing it:

```sh
export DIRECTIONS_GITHUB_CONFIG='{"repository":"owner/repo","number":7,"kind":"pull-request","eventName":"workflow_dispatch","actor":"maintainer","capabilities":"read,review-propose"}'
# Supply GITHUB_TOKEN through your usual secure host credential mechanism.
pi -e /absolute/path/to/directions/tools/pi/github
```

The package declares host-provided typebox/Pi peers and has no private extension
protocol. It is not currently published to npm. Outside the action, *you* must
also restrict the rest of Pi's tools if you need a read-only capability surface;
loading this plugin does not disable an existing unrestricted shell.

For SDK hosts, `createGitHubIntegration(options)` prepares the scoped snapshot
and returns an ordinary `extension` factory, `changedPaths`, and an audit
`records()` accessor. The factory is the same tool implementation used by the
default Pi entry point. Pass only host-approved options; repository instructions
must not configure capabilities, endpoints, actors, or tokens.
