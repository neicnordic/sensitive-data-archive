---
status: accepted
date: 2026-02-12
decision-makers:
  - "@neicnordic/sensitive-data-development-collaboration"
---

# Use Markdown Architectural Decision Records

## Context and Problem Statement

The SDA project is developed by a distributed team across multiple organisations.
Architectural decisions and their rationale are currently scattered across pull
request discussions, GitHub issues, Slack threads, and meeting notes. This makes it
difficult for contributors — both existing and new — to discover *why* the system is
designed the way it is.

We need a lightweight, discoverable way to record architecturally significant
decisions so that the reasoning behind them is preserved alongside the code.

## Decision Drivers

* **Async-first communication** — team members work across time zones; decisions
  must be readable without attending the meeting where they were made.
* **Contributor onboarding** — new contributors should be able to understand past
  design choices without archaeology through old PRs and Slack logs.
* **Markdown-native** — records should live in the repository, render on GitHub, and
  be reviewable in pull requests like any other change.
* **Low ceremony** — the process must be lightweight enough that people actually use
  it; a heavy process will be ignored.

## Considered Options

1. **MADR 4.0.0** — Markdown Any Decision Records with YAML front matter
2. **Nygard template** — Michael Nygard's original ADR format (Title, Status,
   Context, Decision, Consequences)
3. **Y-Statements** — single-sentence structured rationale
4. **Formless** — free-form Markdown files with no template

## Decision Outcome

Chosen option: **MADR 4.0.0**, because it provides enough structure to enable
consistent records and future tooling (via YAML front matter) while remaining
lightweight enough for a small distributed team. The optional-section design means
simple decisions stay simple, while complex decisions can use the full template.

### Consequences

* **Good**: Consistent format across all ADRs makes them easier to read, write, and
  review.
* **Good**: YAML front matter enables future automation (status dashboards, linting,
  index generation).
* **Good**: Living in the repo means decisions are versioned, searchable, and
  co-located with the code they describe.
* **Neutral**: Team members must remember to consider whether a change warrants an
  ADR; the PR template checkbox serves as a nudge.
* **Bad**: Small per-ADR overhead (creating a file, filling in sections, reviewing);
  mitigated by the template and optional sections.

### Confirmation

The existence of this file (ADR-0000) in the repository confirms the decision has
been implemented.

## Pros and Cons of the Options

### MADR 4.0.0

* Good, because YAML front matter enables tooling and metadata queries.
* Good, because optional sections keep simple ADRs short.
* Good, because it is actively maintained with clear versioning.
* Neutral, because the template is slightly longer than Nygard's.

### Nygard template

* Good, because it is the most widely known ADR format.
* Good, because it is very concise (five sections).
* Bad, because it lacks structured metadata (no front matter).
* Bad, because it has no concept of decision drivers or options comparison.

### Y-Statements

* Good, because they are extremely concise (one sentence).
* Bad, because they lack room for context, alternatives, or consequences.
* Bad, because they are too terse for decisions that need explanation.

### Formless

* Good, because there is zero ceremony.
* Bad, because inconsistent structure makes records harder to read and compare.
* Bad, because no metadata means no tooling support.

## More Information

### Conventions Established by This ADR

| Convention | Detail |
| --- | --- |
| Directory | `docs/decisions/` |
| File naming | `NNNN-title-with-dashes.md` — zero-padded, lowercase, dashes between words |
| Numbering | Starts at 0000; numbers are never reused, even for rejected or superseded decisions |
| Status lifecycle | `proposed` → `accepted` \| `rejected` \| `deprecated` \| `superseded` |
| Template | `docs/decisions/template.md` (MADR 4.0.0) |
| Backfilling | Incremental — no mandate to retroactively document every past decision |
| PR nudge | A checkbox in the pull request template reminds authors to consider adding a decision record |

For guidance on when and how to write a decision record, see [`docs/decisions/README.md`](README.md).

The template used here is [MADR 4.0.0](https://github.com/adr/madr/blob/4.0.0/template/adr-template.md).
