# GitHub workflow examples

Copy the workflows you need into the consuming repository's
`.github/workflows/`. These files do not run from `examples/github/`.
Directions itself ships with no active workflows, including no manual triggers.

These examples use **one Agent action** for on-demand review and issue planning,
with different profiles and explicit capabilities.

## Adoption

1. Review and copy the example `.directions/` directory alongside this README,
   or merge its review/plan/describe profiles into your existing configuration.
   Do not overwrite repository guidance blindly.
2. After publishing Directions, pin **every** `REPLACE_WITH_FULL_COMMIT_SHA`
   to a reviewed commit of `jataware/directions`. The examples call the
   standalone `jataware/directions/agent` action.
3. Provide `OPENAI_API_KEY` and review the chosen model. Other supported
   catalog providers can use the same generic `api-key` action input.
4. Review workflow permissions and GitHub's repository settings. The default
   workflow token is used; a separately scoped bot token can be passed explicitly
   if your repository requires different authorship or permissions.
5. Enable only selected workflows. Test from a maintainer-owned issue/PR first.
   Do not enable old and new handlers for the same slash command simultaneously.

Guidance is checked out from the trusted default branch with persisted Git
credentials disabled. PR source comes from pinned GitHub blobs; no PR scripts,
package installs, tests, local Pi settings, or extensions are executed.

## Available examples

| Workflow | Trigger | Effects |
| --- | --- | --- |
| ci.yml | Push to trunk, PR, or manual dispatch after installation | Runs local checks and fake-service tests; no model secrets required |
| reviewer.yml | `/directions review` on a PR/general or inline comment | Proposes and publishes one COMMENT review, after actor/head/base checks |
| review-proposal.yml | Same-repository PR opened/updated | Retains a review proposal in artifacts; cannot publish |
| issue-plan.yml | `/directions plan` on an issue | Reads source/discussion and publishes a new plan comment |
| pr-describe-proposal.yml | `/directions describe` on a PR | Produces proposed description Markdown in response.md; **does not replace the PR body** |
| change-plan.yml | Manual dispatch with a PR number | Produces a focused follow-up implementation/code-change proposal; **does not edit code** |

The two review examples can coexist because they have distinct triggers and
only the explicit command workflow can publish. If you only want on-demand
spending, omit review-proposal.yml. Forks do not receive model secrets from the
automatic example; explicit maintainer commands remain subject to plugin checks.
Submitted-review events are not slash-command triggers.

Root context is always read alongside the selected profile. Profiles inherit
all policies by default; whitelist or blacklist selectors restrict eligibility,
and triggers control relevance. The example description profile whitelists only
the evidence policy. Workflow inputs select executable capabilities. A repository
policy cannot turn a proposal-only workflow into a publisher.

## Scope

These workflows do not edit or push repository code, create branches/PRs, or
replace existing issue/PR descriptions. Description and change-planning workflows
produce artifacts for human review. General PR reviews and new issue-plan comments
can be published only by workflows with the corresponding capabilities.

Inline review findings, replies, and thread resolution are not yet implemented.

## Artifacts and publication

The agent workflows explicitly upload the output directory, including failure
diagnostics, with short retention. Artifacts may contain private code, PR/issue
text, proposed reviews, and API receipts. Review access/retention before adoption.

A final model message is not proof of a publication. `github.json` distinguishes
proposal, publication-attempt, and published receipts. A failed run may already
have published something before a later model failure; inspect receipts before
rerunning. Publication is not atomic against concurrent GitHub edits.

## Additional integrations

See [the action README](../../agent/README.md#load-another-ordinary-pi-extension).
Supply approved local Pi package paths outside the inspected workspace, with
dependencies installed separately from reviewed locks. Integration configuration
and credentials are workflow-owned, never taken from PR-controlled Directions
files. Service-specific integrations belong in separately approved extensions.
