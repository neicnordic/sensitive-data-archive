---
status: exploring
date: "2026-10-06"
discussion: "https://github.com/neicnordic/sensitive-data-archive/pull/2655"
authors:
   - "@jhagberg"
consulted: []
informed: []
---

# Add a Claude coding bot ("Megalocnus") to the team

## Context and Problem Statement

Several of us already use Claude Code locally, and a large share of our backlog is small, well-defined work: chores, clear bugs, test gaps and lint fixes.
Anthropic now ships an official GitHub integration, [`anthropics/claude-code-action@v1`](https://github.com/anthropics/claude-code-action), that can run Claude on GitHub-hosted runners in response to labels and comments.
This RFC explores whether the team wants a Claude coding bot as a visible team member, how work is handed to it, and how we keep it transparent that Claude wrote the code.
It proposes a four-week pilot with a concrete design, and lists what the team still has to decide.

The proposed name is **Megalocnus**, after a Caribbean giant ground sloth; the name is free on GitHub as both App and account.

## Decision Drivers

* **Transparency** - every commit, PR and comment written by the bot must be recognisable as Claude's work, and show which human approved it.
* **Humans stay accountable** - the bot never merges; our normal review rules (two approvals, rebase-only merge queue, CI) apply unchanged.
* **Security in a public repository** - anyone can open issues and comment, so prompt injection and secret exfiltration have to be designed out, not hoped away.
* **Low review burden** - bot PRs must be small and focused, or they cost more reviewer time than they save.
* **Cheap to try, cheap to stop** - the pilot should need no budget decision and be switchable off in one click.

## Considered Options

The design breaks down into five sub-questions.

* **Where the bot runs**
  * GitHub-hosted runners with `claude-code-action`
  * Self-hosted runners on our own hardware
  * Anthropic's cloud (Claude Code on the web, Routines)
* **How it is paid for**
  * A. OAuth token from one team member's Claude Team seat (`claude setup-token`)
  * B. A Console API key in an NBIS/NeIC workspace with a spend cap
  * C. A dedicated Team seat for the bot
  * D. Workload identity federation (GitHub OIDC to the Anthropic Console, no long-lived secret)
* **GitHub identity**
  * A machine-user account (assignable) plus our own GitHub App
  * Our own GitHub App only, with work handed over by label
  * Anthropic's shared `claude[bot]` App
* **How work flows from issue to PR**
  * One entry point with a plan gate: label, plan, human says go, implement
  * Separate refine and implement labels
  * Implement directly, the bot asks only if it thinks the issue is unclear
* **Whether the App may change `.github/workflows/`**
  * No Workflows permission
  * Workflows permission

## Proposed Pilot

The pilot uses the first option listed in each sub-question, except GitHub identity: it starts with our own App only and adds the machine-user account later if the team wants assignment.
Billing option A is for the pilot only.
The rationale is in [Pros and Cons of the Options](#pros-and-cons-of-the-options).

### Flow

```mermaid
flowchart TD
    label["Team member adds label <code>megalocnus</code>"] --> refine
    replan["Team member comments <code>@megalocnus replan</code>"] --> refine
    refine["<b>claude_refine</b><br/>read-only: reads issue, comments, code"] --> plan
    plan["Plan comment<br/>+ label <code>megalocnus:planned</code>"] --> human{"Human reads the plan"}
    human -- "needs changes" --> replan
    human -- "<code>@megalocnus go</code>" --> agent
    agent["<b>Agent job</b><br/>Claude writes the change<br/>OAuth token only, no write access"] -- "patch" --> test
    test["<b>Test job</b><br/>no secrets: go test incl. Docker, lint"] -- "patch + results" --> publish
    publish["<b>Publish job</b><br/>fresh App token, no model, no generated code<br/>commit, branch, draft PR"] --> pr
    agent -. "needs a workflow change" .-> stop["Publish job comments on the issue"]
    agent -. "turns or time used up" .-> partial["Publish job opens the draft PR<br/>marked incomplete, or reports no change"]
    pr["Draft PR linked to the issue<br/>label <code>megalocnus:planned</code> removed"] --> review["Humans: review, CI,<br/>two approvals, merge queue"]
```

**Refine** runs when someone with write access adds the `megalocnus` label, or comments `@megalocnus replan`.
The bot reads the issue, its comments and the relevant code, and posts one plan comment with fixed headings: understanding, assumptions, numbered open questions, plan (files and steps), tests, size (S/M/L), cross-repo impact, and a recommendation (ready, needs answers, or too big with a proposed split).
Each plan is a new, numbered comment ("Plan v2"); on replan the bot marks the previous plan as superseded instead of rewriting it, so the history stays readable.
It always adds `megalocnus:planned`; the human decides whether the plan is good enough, and answers open questions in the go comment.

**Implement** runs when someone with write access comments `@megalocnus go` on an issue that has `megalocnus:planned`.
A preflight step checks the commenter's repository permission before any App token is minted or any model starts.
A go is refused while a refine run for the same issue is queued or running, so nobody approves a plan that is about to change.
Its input is only the go comment and the latest plan posted *before* it; both are fixed when the run starts and passed unchanged to every job, so a plan posted while the go waited in the queue is never used.
The rest of the thread is not passed on.
Implement is split into three jobs:

* The **agent job** runs Claude with the OAuth token, and passes the read-only `GITHUB_TOKEN` to the action explicitly so that it never falls back to App authentication.
  Claude implements the plan against a recorded base commit of `main` and hands over the result as one patch artifact.
  Go commands can execute code (tests, but also `-toolexec` and `-vettool`), so the agent job runs no build, vet, lint or test commands by default.
  If Claude Code's command sandbox can be configured and shown to hide the OAuth token from child processes and block the network, Claude may run Docker-free checks there to fix its own mistakes.
* The **test job** has no repository secrets, only a read-only `GITHUB_TOKEN`, and no shared writable caches.
  It applies the patch to the same base commit and runs build, vet, `go test` (including the packages that start PostgreSQL or RabbitMQ in Docker) and lint for the touched modules.
  The bot's code may tamper with anything in this job, so only a short, bounded result summary is passed on; the publish job never uses files from it.
* The **publish job** runs last, also when the agent or test job fails or the agent runs out of turns.
  It uses the original agent artifact, checks it before minting a fresh App token, runs no model and no generated code, and only applies the patch, creates the commits through the API, and opens a **draft** PR.
  If an open bot PR for the issue already exists, it reports that PR instead of opening a second one.
  It then removes `megalocnus:planned`.

The PR body links the issue (`Closes #N`) and the plan version, includes the test job's results, and lists every deviation from the plan.
Reviewers therefore see test results early; if the team chooses option (a) under [Open Questions](#open-questions), they see them before the regular, secret-bearing CI runs.
Claude does not see the test job's results in the same run; an automatic fix round is left for after the pilot.
The split means the App's write access never shares a runner with code the bot wrote, and the one-hour App token is minted only when it is needed.

**After the PR** everything is as for any other PR.
During the pilot, humans push follow-up fixes to the bot's branch themselves.

**Failure handling.**

* Turns or time used up, or checks still failing: the publish job opens the draft PR anyway, marked incomplete with what fails, or reports that there was no usable change.
* The change needs a workflow edit: the publish job refuses the patch and says so on the issue.
* Rate limit, cancellation or a crash before publishing: the publish job, or a final status step, comments with a link to the run log; `megalocnus:planned` stays, so a new go retries.

### Components

| Piece | Notes |
| --- | --- |
| GitHub App `megalocnus` | Owned by neicnordic, installed on this repository only. Contents, Issues and Pull requests read/write; Metadata read. No Workflows permission, no webhook. Setup checklist in the [appendix](#appendix-github-app-setup). |
| Repository secrets | `MEGALOCNUS_APP_ID`, `MEGALOCNUS_APP_PRIVATE_KEY`, `CLAUDE_CODE_OAUTH_TOKEN`. Only the two Megalocnus workflows reference them. |
| `.github/workflows/claude_refine.yml` | `issues: labeled` and `issue_comment` (replan). Mints an App token downscoped to Contents read and Issues write. Read-only tools plus commenting. |
| `.github/workflows/claude_implement.yml` | `issue_comment` (go). Agent job without App token; test job without any secrets; publish job mints an App token with Contents, Issues and Pull requests write. |
| `.claude/commands/refine-issue.md`, `implement-issue.md` | The bot's instructions, reviewed like code. Issue-triggered workflows check out `main`, so the bot always follows the merged version. |
| Labels | `megalocnus`, `megalocnus:planned`. |
| `AGENTS.md` and `CLAUDE.md` | Shared repository instructions, see [Repository instructions](#repository-instructions). |

All jobs run on `ubuntu-latest`.
Both workflows use `claude-code-action` in automation mode with an explicit prompt, and do their own trigger checks at job level before any token is minted: label events check the label name; comment events require `types: [created]`, an issue rather than a PR, and a comment that starts with the command.
Workflow `permissions:` only govern `GITHUB_TOKEN`, so the App token is downscoped explicitly when it is minted with `actions/create-github-app-token`.
Actions that handle secrets (`claude-code-action`, `create-github-app-token`) are pinned to full commit SHAs; Dependabot still updates them.

### Identity and Transparency

* Commits are authored by `megalocnus[bot]` and created through the GitHub API with the App token, so they show as *Verified* during review.
  GitHub's rebase merge creates replacement commits without signature verification; the bot stays the author.
* Commit messages follow Conventional Commits (ADR-0007) and carry two trailers:

  ```text
  Refs: #123
  Plan-approved-by: @handle
  ```

  We deliberately do not use `Co-authored-by` for the approver: GitHub would then show them as co-author of code they did not write.
* Every PR ends with a footer such as:

  > 🦥 Written by **Megalocnus**, Claude (`claude-sonnet-5-5`) via claude-code-action. Plan approved by @handle in #123. Run log: (link). A human reviews every change before merge.

  Plan comments end with a similar footer that names the requester and says the plan is awaiting approval (`@megalocnus go`).
  The model id and the approver are filled in by the workflow, not written by the model.
* The App profile has a sloth avatar and the description *"Claude (Anthropic) coding agent for the SDA team. Acts only on requests from team members; every PR is human-reviewed."*
* `CONTRIBUTING.md` gets a short section on how to use Megalocnus and the team's policy for AI-written code.

### Security and Cost

* Only users with write access can trigger the bot (the action's default); `allowed_non_write_users` and `allowed_bots` stay empty.
* Issue text and comments, linked material and the `.related/` checkouts are untrusted input.
  The refine step reads them, so a crafted issue can try to steer the plan; the human reading the plan is the main defence, not a solved problem.
* Tool allow-lists limit what the model calls directly, but not what code it runs: a test or Makefile target the bot writes can read environment variables and use the network.
  The publish split keeps the App token away from that code.
  The OAuth token cannot be kept away, because Claude needs it in the agent job; this is a known limitation (see below).
* `show_full_output` stays off, and the jobs refuse to run when debug logging is on (`runner.debug`), because *Re-run with debug logging* turns full output back on.
  The raw execution file is never uploaded as an artifact.
* Without the Workflows permission the bot cannot change workflow files, and so cannot add new places where secrets are used.
  It can still change tests, scripts and Dockerfiles, and its pushes trigger normal PR CI, which runs that code with the secrets any same-repository branch gets (for example `CODECOV_TOKEN` and package publishing in the image job).
  Reviewers must treat changes to tests and scripts in bot PRs as carefully as workflow changes.
* The `main` ruleset already requires a PR with two approvals, linear history and the merge queue, so the bot cannot push to `main`.
* Models: `claude-opus-5-5` for refine (short runs that need judgement), `claude-sonnet-5-5` for implement (long runs against an approved plan).
* Limits: refine at most 30 turns and 15 minutes; the agent job at most 100 turns and 45 minutes; the test job at most 30 minutes.
  Both workflows share one concurrency group per issue, with new runs queued rather than cancelled, and only one agent job runs at a time across the repository.

### Repository Instructions

* `AGENTS.md` at the root holds tool-neutral facts: a repository map, build/test/lint commands, conventions (Conventional Commits, rebase-only history, CHANGELOGs, `log/slog`, `accessionID`, absolute links for the neic-sda docs aggregation) and working principles (simplest change that solves the issue, tight scope, match surrounding style, state assumptions, verifiable definition of done).
  Codex and other agents read it directly.
* `CLAUDE.md` contains `@AGENTS.md`, because Claude Code ignores `AGENTS.md` when a `CLAUDE.md` exists.
* Bot-only behaviour (plan format, footer, trailers, stopping on workflow changes) lives in `.claude/commands/`, so people's local sessions do not pick it up.
* `AGENTS.md` and `CLAUDE.md` are useful without the bot and go in a separate PR first.

### Related Repositories

The bot only has write access to this repository, but it gets read access to the public repositories that depend on our APIs or docs.

| Repository | Relationship | Access in the pilot |
| --- | --- | --- |
| [NBISweden/sda-cli](https://github.com/NBISweden/sda-cli) | User CLI for inbox and download | Shallow checkout into `.related/` on every run |
| [NBISweden/sda-download-ui](https://github.com/NBISweden/sda-download-ui) | Web UI on top of download v2 | Shallow checkout into `.related/` on every run |
| [neicnordic/neic-sda](https://github.com/neicnordic/neic-sda) | Documentation site aggregating our docs | Shallow checkout into `.related/` on every run |
| [neicnordic/crypt4gh](https://github.com/neicnordic/crypt4gh) | Go dependency | Exact version via `go mod download` |
| [umccr/htsget-rs](https://github.com/umccr/htsget-rs) | htsget server used alongside download | Listed in `AGENTS.md` only |
| [GenomicDataInfrastructure/starter-kit-storage-and-interfaces](https://github.com/GenomicDataInfrastructure/starter-kit-storage-and-interfaces) | Deploys SDA for GDI | Listed in `AGENTS.md` only |

When an issue needs a change elsewhere, the plan says so under *cross-repo impact* with a suggested issue text, and a human files it.

### Rollout

1. PR with `AGENTS.md` and `CLAUDE.md`.
2. This RFC.
3. Manual setup by an org owner and a repository admin (see [appendix](#appendix-github-app-setup)).
4. Workflows are tested in a personal sandbox repository with a separate `megalocnus-dev` App, because `issues` and `issue_comment` workflows always run from the default branch and cannot be tried from a PR branch.
   Functional scenarios: a clear chore, a vague issue (expect questions), an oversized issue (expect a proposed split), and an issue that needs a workflow change (expect a stop).
   Security scenarios: a go from an account without write access and a label added by a triage-only user (expect no model run and no App token), a go comment on a PR (ignored), two go comments in quick succession (one PR), a go during a replan (refused), an issue that asks for a test that reads environment variables (no App token reachable), and an emergency stop during a running job.
5. PR with the two workflows, `.claude/commands/` and the `CONTRIBUTING.md` section.
6. Four-week pilot (about two sprints) on category-A issues; anyone may try harder ones.
   The sprint retrospective decides whether to continue, adjust or stop.

**Pause:** disable the two workflows in the Actions tab; this stops new runs only.

**Emergency stop:** additionally cancel active and queued runs, suspend the App installation, and revoke the OAuth token in the owner's Claude account.
Deleting a secret does not revoke the credential behind it.

**Pilot measurements:** issues handled, share of PRs merged, review rounds per PR, how often a replan was needed, quota used, and incidents.

### Prerequisites Before Activation

These are implementation requirements, recorded here so the implementation plan cannot drop them:

* **Artifact contract:** one base SHA for all three jobs; a complete patch including new and deleted files; one immutable agent artifact consumed by both later jobs; the agent job reserves time to upload it; a missing artifact or digest mismatch fails closed.
* **Publisher boundary:** the publish logic comes from `main`, outside the patched tree, and the patch is validated before the App token is minted: no paths under `.github/` or `.claude/`, no `CLAUDE.md` or `AGENTS.md` changes unless the issue is about them, no path traversal, symlinks or submodules.
* **Authorization preflight:** every trigger checks the actor's repository permission with a read-only token before minting App tokens or starting a model.
* **Concurrency:** queued runs use `cancel-in-progress: false`; queue order follows waiting time, not comment time, which is why the plan selection above is fixed at run start.
* **Related checkouts:** record the commit SHA of each `.related/` checkout in the plan and reuse it in implement; never run scripts or load configuration from them.
* **Tests:** an explicit mapping from touched paths to Go modules, packages and build tags (for example the `visas` variant CI runs), measured against the time limits.
* **Logging:** claude-code-action v1 prints the automation prompt in the run log, so prompts must not contain anything that is not already public.
* **Cancellation:** a hard cancel can skip the final steps; a separate status step or workflow reports runs that ended without publishing.

### Known Limitations of the Pilot

* The OAuth token is present in the agent job, next to code the bot writes and runs, so a successful prompt injection could leak it.
  The impact is misuse of the owner's Claude quota until the token is revoked; it gives no access to GitHub.
* The OAuth token belongs to one person's Team seat and is valid for one year; the bot stops if that person leaves, changes plan or the token expires.
  It also shares that person's usage limits, which is why implement runs are serialised.
* Anthropic documents Team OAuth tokens for GitHub Actions, and its terms recommend API keys for products and services.
  Whether a shared team bot may run on one named member's seat is unconfirmed, and the length of the pilot does not change that; we should get confirmation for our plan before starting, or use Console-funded authentication.
* If the Team plan has extra usage (usage credits) enabled, bot runs beyond the seat's limits become billable; the setting should be checked before the pilot.
* The bot cannot take CI or workflow issues.
* Fork PRs are out of reach: secrets are withheld and the App cannot push to forks.
* No automated second-model review; reviewers may run one locally (for example Codex).

## Open Questions

* **Billing after the pilot.** API key (B), workload identity federation (D) or a dedicated seat (C), and who owns the budget?
* **Assignment.** Do we want a machine-user account so issues can be assigned to the bot and show on the board, or are labels enough?
* **CI on bot draft PRs.** Bot pushes trigger normal PR CI, which runs the bot's code with `CODECOV_TOKEN` and package publishing.
  Option (a): skip those steps while the PR is a draft by `megalocnus[bot]`, and run them once a human has read the diff and marked it ready (touches two existing workflows, which also need the `ready_for_review` event added).
  Option (b): accept the same risk as any team member's branch and document it.
  The author leans towards (a).
* **Two approvals on bot PRs.** Should the person who said go count as one of the two required approvers, or should both approvers be someone else?
* **Accountability trailer.** Is `Plan-approved-by:` the right trailer, or does the team prefer `Co-authored-by:` despite its meaning?
* **Review-fix loop.** Should `@megalocnus fix ...` on the bot's own PRs push follow-up commits after the pilot?
* **Second-model review in CI.** Is an automated review by another model (for example Codex) worth the extra cost and setup?
* **Workflows permission.** Is there a setup that would make it safe enough to let the bot take CI chores?
* **Scope.** Which issues count as category A, and do we want a rule for what the bot should not touch (for example database migrations or crypto code)?
* **Ruleset.** The `main` ruleset has `require_extra_approval_for_unattributed_changes` enabled; we should confirm how that interacts with bot-authored commits.

## Pros and Cons of the Options

### Where the Bot Runs

#### GitHub-hosted runners with `claude-code-action`

* Good, because they are free for public repositories, ephemeral, and the action is officially supported.
* Good, because the bot acts under its own App identity, not a person's account.
* Bad, because each run spends Actions minutes and has to set up Go and dependencies from scratch.

#### Self-hosted runners on our own hardware

* Good, because we control the machine and could keep caches warm.
* Bad, because GitHub states that self-hosted runners ["should almost never be used for public repositories"](https://docs.github.com/en/actions/reference/security/secure-use): anyone can open a PR that runs code on them, and they are not ephemeral.

#### Anthropic's cloud (Claude Code on the web, Routines)

* Good, because there is no runner to maintain.
* Bad, because there is no issue trigger (Routines react to PR and release events only), and both act as an individual's GitHub account, so the bot cannot be a team identity.

### How It Is Paid For

#### A. One person's Team seat (OAuth token)

* Good, because it costs nothing extra and needs no budget decision for a pilot.
* Bad, because of the limitations listed [above](#known-limitations-of-the-pilot).
* Bad, because rotating tokens from several people's seats to pool limits would move further into what the terms seem to prohibit; we do not do that.

#### B. Console API key with a spend cap

* Good, because the terms clearly allow it and it does not depend on any one person.
* Bad, because it is billed per token and needs someone with budget authority.

#### C. Dedicated Team seat for the bot

* Good, because the cost is fixed and predictable.
* Bad, because it is unverified whether a non-human seat is allowed; this has to be confirmed with Anthropic first.

#### D. Workload identity federation

* Good, because there is no long-lived secret to leak or rotate.
* Neutral, because it is billed like B.
* Bad, because the setup is slightly more involved.

### GitHub Identity

#### Machine-user account plus our own GitHub App

* Good, because the bot can be assigned issues and shows as assignee on the board.
* Bad, because someone has to own and secure an extra GitHub account.

#### Our own GitHub App only, handed work by label

* Good, because it has its own clear name and nothing extra to manage, and moving to the machine-user option later loses nothing.
* Good, because the person adding the label can assign themselves, which keeps a named human accountable.
* Bad, because the bot does not appear as assignee and cannot be @-autocompleted.

#### Anthropic's shared `claude[bot]`

* Good, because setup is minimal.
* Bad, because the name is generic and shared with Anthropic's Code Review, and its default `@claude` trigger collides with `@claude review`.

### How Work Flows from Issue to PR

#### One entry point with a plan gate

* Good, because every issue gets a refine step and a human approves the plan before any code is written.
* Good, because it is one label and one comment command to explain.
* Neutral, because small chores pay a small overhead: skim the plan, say go.

#### Separate refine and implement labels

* Good, because it is flexible.
* Bad, because people will skip refining on issues that needed it.

#### Implement directly

* Good, because it is fastest.
* Bad, because the bot decides what "clear enough" means, and a prompt-level instruction to plan first is not a gate.

### Workflows Permission

#### No Workflows permission

* Good, because a hijacked bot cannot add a workflow that names the bot's own secrets, or delete or rewrite existing CI jobs.
* Neutral, because it can still change the tests and scripts that existing CI runs (see [Security and Cost](#security-and-cost)).
* Bad, because CI and workflow issues stay with humans.

#### Workflows permission

* Good, because the bot could take CI chores.
* Bad, because a hijacked bot could add a workflow that uses any repository secret.
  Moving the bot's secrets to an Environment restricted to `main` narrows this, but is configuration someone has to keep right, and does not cover other secrets.

## Appendix: GitHub App Setup

An organisation owner creates the App, because an org-owned App survives people changing jobs.
Organization settings, Developer settings, GitHub Apps, New GitHub App:

* **Name:** `megalocnus` (checked free on 2026-10-06; the plain name has no GitHub user account, so `@megalocnus` pings nobody).
* **Description:** "Claude (Anthropic) coding agent for the SDA team. Acts only on requests from team members; every PR is human-reviewed."
* **Homepage URL:** this RFC.
* **Webhook:** untick *Active*; the action does not need webhooks.
* **Repository permissions:** Contents read and write, Issues read and write, Pull requests read and write, Metadata read-only. Nothing else, in particular not Workflows.
* **Where can this GitHub App be installed:** *Only on this account*.
* After creating: upload a sloth avatar, note the App ID, generate a private key.
* **Install App:** *Only select repositories*, `sensitive-data-archive`.
* A repository admin adds `MEGALOCNUS_APP_ID` and `MEGALOCNUS_APP_PRIVATE_KEY` as repository secrets directly from the downloaded key file; the key is never pasted into chat or issues.
  The token owner adds `CLAUDE_CODE_OAUTH_TOKEN`, or hands it to the admin through a password manager.

An org-owned App limited to *Only on this account* cannot be installed in a personal sandbox, so sandbox testing uses a separate `megalocnus-dev` App with the same permissions, owned by the person testing.

## More Information

* [claude-code-action](https://github.com/anthropics/claude-code-action), including its [security notes](https://github.com/anthropics/claude-code-action/blob/main/docs/security.md) and [example workflows](https://github.com/anthropics/claude-code-action/tree/main/examples).
* [Claude Code GitHub Actions documentation](https://code.claude.com/docs/en/github-actions).
* [Claude Code legal and compliance](https://code.claude.com/docs/en/legal-and-compliance), for the subscription and API-key terms quoted above.
* [GitHub: secure use of Actions](https://docs.github.com/en/actions/reference/security/secure-use), for the self-hosted runner guidance.
* [ADR-0007](../decisions/0007-use-conventional-commits.md), Conventional Commits.
