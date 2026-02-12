# Decisions

This directory captures the decisions made in the SDA project. For the full
rationale behind adopting this practice, see
[ADR-0000](0000-use-markdown-architectural-decision-records.md).

## Philosophy

Decision records are a way for the team to think smarter and communicate
better — they are not after-the-fact paperwork. Focus on the *"why"*, not the
*"what"*. A good decision record helps a future contributor (or your future self)
understand the reasoning behind a choice.

Decisions are not limited to software architecture. Technology choices, vendor
decisions, conventions, and process changes all belong here if they are worth
explaining to someone who wasn't in the room.

Anyone can propose a decision record. If you think future contributors will
wonder *"why did we do it this way?"*, that is a good signal to write one.

## When to write a decision record

Consider writing one when a change involves:

* Structural changes to the codebase (new services, module boundaries)
* Technology or library choices
* Conventions and standards (coding style, API design, naming)
* Non-obvious trade-offs that future contributors will wonder about

Each decision record should cover a single decision — not a bundle of
unrelated choices.

Skip a decision record when the choice is small-scoped, easily reversible, and
already obvious from the code or commit message.

Not every change needs a record — use your judgement. The pull request template
includes a checkbox as a reminder.

## How to create a decision record

1. Copy the template:
   ```sh
   cp docs/decisions/template.md docs/decisions/NNNN-title-with-dashes.md
   ```
   Use the next available number, zero-padded to four digits.

2. Fill in the sections. Remove any optional sections (marked by HTML comments)
   you do not need.

3. Set `status: proposed` in the front matter.

4. Open a pull request. The decision record is accepted when the PR is merged
   (update the status to `accepted`).

## Tips for good decision records

* **Focus on rationale** — explain *why* the decision was made, not just *what*
  was decided. The "what" is visible in the code; the "why" is not.
* **Be specific** — each record should address one decision with enough context
  to stand on its own.
* **Keep them living documents** — if circumstances change, update the record
  with a date-stamped note rather than letting it silently go stale. For major
  reversals, create a new record and mark the old one as `superseded`.

## Status lifecycle

| Status | Meaning |
|---|---|
| `proposed` | Under discussion in a PR |
| `accepted` | Merged and in effect |
| `rejected` | Considered but not adopted |
| `deprecated` | No longer applies (but kept for history) |
| `superseded` | Replaced by a newer decision record (link to it in the front matter) |

Numbers are never reused. Superseded and rejected records stay in the directory
for historical context.

**To supersede a decision**: create the new record, then update the old record's
front matter to `status: superseded` and set `superseded-by` to the new file name:

```yaml
status: superseded
superseded-by: "0005-use-new-approach.md"
```

## Index

| # | Decision | Status |
|---|----------|--------|
| [0000](0000-use-markdown-architectural-decision-records.md) | Use Markdown Architectural Decision Records | accepted |

## More information

The template follows [MADR 4.0.0](https://github.com/adr/madr/blob/4.0.0/template/adr-template.md).
