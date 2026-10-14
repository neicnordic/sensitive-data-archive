---
status: exploring
date: "2026-10-14"
discussion: "https://github.com/neicnordic/sensitive-data-archive/pull/2655"
authors:
   - "@jhagberg"
consulted: []
informed: []
---

# Let a Claude bot ("Megalocnus") refine issues

## Context and Problem Statement

Many of our issues reach refinement half-specified, and the first part of every refinement is finding out what is unclear.
Several of us already use Claude Code locally for that kind of work.
This RFC proposes a four-week pilot where a Claude bot reads each new issue and the code it touches, and posts a plan with clarifying questions *before* the team refines it.
The bot writes no code; whoever picks up the issue implements it as usual.

## How the pilot works

```mermaid
flowchart LR
    new["Team member opens an issue<br/>or adds label <code>megalocnus</code>"] --> bot["Megalocnus reads the issue and the code<br/>(read-only, runs nothing)"]
    bot --> plan["Plan comment with<br/>numbered questions"]
    plan --> answer["Team answers in the issue"]
    answer -- "<code>@megalocnus replan</code>" --> bot
    plan --> refinement["Human refinement as usual"]
```

* **Trigger:** a team member with write access opens an issue, adds the `megalocnus` label, or comments `@megalocnus replan`. Issues opened by people outside the team only get a plan if a team member adds the label.
* **Output:** one comment per run, with the same headings every time: understanding, assumptions, numbered questions, proposed plan (files and steps), tests, size (S/M/L), impact on other repositories, and whether the issue is ready, needs answers, or should be split. A replan posts "Plan v2" and marks the old one as superseded.
* **Transparency:** comments come from `megalocnus[bot]` and end with a footer naming the model and saying that nothing has been changed and a human decides.
* **Runs on** GitHub-hosted runners with Anthropic's official [`claude-code-action`](https://github.com/anthropics/claude-code-action). The `megalocnus` GitHub App is already registered by @viklund; the bot's instructions live in `.claude/commands/` and follow the repository's [`AGENTS.md`](https://github.com/neicnordic/sensitive-data-archive/pull/2662).

## Why plan-only first

* It is the part we can try safely. The bot can only read files and comment: it has no shell, runs no code and cannot push, so a crafted issue can at worst produce a misleading comment, which a human reads anyway.
* It needs one workflow and one prompt file, not the machinery that letting a bot write and test code in a public repository requires.
* It tells us cheaply whether the bot's understanding of our code is good enough before anyone discusses letting it write code.

A design for a second phase, where the bot also opens draft PRs after a human says go, exists and will be proposed separately if the pilot is useful.

## Cost and billing

The pilot would run on @jhagberg's Claude Team seat, which Anthropic documents for GitHub Actions. Whether one person's seat may back a bot the whole team uses is not confirmed, so we should check that before starting or use a Console API key instead. About 40 issues a month are opened today, and a plan run is short.

## Rollout

1. `AGENTS.md` (#2662), so the bot and our local agents share the same repository conventions.
2. This RFC.
3. One workflow and one prompt file, tried in a sandbox repository first, then a PR here.
4. Four weeks; the sprint retrospective decides whether to continue, change or stop.

To stop the bot, disable the workflow in the Actions tab.

## Open Questions

* **Every new issue, or only labelled ones?** Plans on every team-opened issue give the most data, but may be noise on issues that are already detailed.
* **What should the plan contain?** Are the headings above right, and is anything missing that we always ask in refinement?
* **Billing after the pilot:** a Console API key with a spend cap, or another option, and who owns that budget?
* **Phase 2:** if the plans are good, do we want the bot to also open draft PRs?

## Considered Options

* **Plan-only pilot (proposed).**
* **Plan and implement in one pilot.** Rejected for now: safely running bot-written code with write access in a public repository needs several isolated jobs and a long list of safeguards, which is a lot to build before we know whether the plans are useful.
* **No bot; refine as today.** Costs nothing, but we learn nothing.

Where to run it: self-hosted runners are ruled out because GitHub [advises against them for public repositories](https://docs.github.com/en/actions/reference/security/secure-use), and Anthropic's cloud sessions cannot be triggered from issues and act as a person rather than a team bot.

## More Information

* [claude-code-action security notes](https://github.com/anthropics/claude-code-action/blob/main/docs/security.md)
* [Claude Code GitHub Actions documentation](https://code.claude.com/docs/en/github-actions)
