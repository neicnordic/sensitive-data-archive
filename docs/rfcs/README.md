# Requests for Comments (RFCs)

This directory is an **upstream exploration phase** for architectural work.
It is not a decision log. For the decision log, see
[`docs/decisions/`](../decisions/).

An RFC is the right place to put a question the team is actively exploring but
cannot yet resolve with *"Chosen option: X, because Y"*. When an RFC matures
to that point, it is **promoted** to an ADR. The RFC file stays in this
directory as a frozen reference to the exploration; the ADR is a new file in
`docs/decisions/`.

For the full rationale, see
[ADR-0005](../decisions/0005-introduce-rfcs-as-upstream-exploration-phase.md).

## When to write an RFC

Write an RFC when all of the following are true:

* The team recognises a question that will benefit from written discussion.
* You can describe the problem, but **cannot yet commit** to one option.
* The discussion will plausibly take longer than a typical ADR PR — days or
  weeks, not hours.

Skip the RFC and open an ADR directly when the team has already converged and
you can write *"Chosen option: X, because Y"* in the initial PR.

## How to write an RFC

1. Copy the template:

   ```sh
   cp docs/rfcs/template.md docs/rfcs/NNNN-title-with-dashes.md
   ```

   Use the next available RFC number, zero-padded to four digits. RFC and ADR
   numbering are independent.

2. Fill in the sections. The `## Open Questions` section is the heart of an
   RFC — not a placeholder, it is the deliverable.

3. Set `status: exploring` in the front matter.

4. Open a pull request and label it `rfc`.

5. The RFC is merged in its current state so the exploration can be versioned.
   It does **not** have to reach a conclusion before merging. `exploring` is a
   valid end state for a long time.

## Status lifecycle

| Status | Meaning |
| --- | --- |
| `exploring` | Under active exploration. Open questions are expected. |
| `ready-for-decision` | The team believes an ADR can now be written from this RFC. |
| `promoted` | One or more ADRs have been created from this RFC. The RFC body is frozen; `promoted-to` lists the resulting ADR filenames. |
| `withdrawn` | No longer pursued. Kept in place for history. |

RFC numbers are never reused. Once an RFC has any status other than `exploring`,
its body should not be edited further — only `status`, `date`, `promoted-to`,
and index entries change.

## Promotion — RFC → ADR

An RFC is ready to be promoted when the team can honestly write
*"Chosen option: X, because Y"*. When that is true:

1. Create the ADR file at `docs/decisions/NNNN-title-with-dashes.md` using the
   next free ADR number (see the [ADR index](../decisions/README.md#index) —
   some numbers may already be reserved by open ADR PRs). Start from
   [`docs/decisions/template.md`](../decisions/template.md). Pull over the
   Context, Considered Options, and the chosen option; drop the Open Questions
   that the decision resolves.
2. Add `## Decision Outcome` with *"Chosen option: X, because Y"*, and
   `### Confirmation` if applicable.
3. In the original RFC file: flip `status` from `exploring` or
   `ready-for-decision` to `promoted`, set `promoted-to` to the ADR filename
   (or a list of filenames, if the RFC yielded more than one decision), and
   update the `date`.
4. **Do not edit the RFC body after promotion.** The RFC is a frozen artifact
   of the exploration that preceded the decision. If the ADR later needs
   revision, revise the ADR, not the RFC.
5. Update both indices:
   * the RFC row in [this README's index](#index) — mark as `promoted`, link
     to the ADR;
   * the ADR row in [`docs/decisions/README.md`](../decisions/README.md#index).

Promotion is complete only when both index updates have been made.

The ADR PR that follows promotion is expected to merge quickly. If it stalls,
that is a signal that the exploration was not complete — consider reopening
the question as a new RFC rather than leaving the ADR `proposed` indefinitely.

### Edge cases

**One RFC, several decisions.** If the exploration reveals more than one
decision the team is ready to make, create one ADR per decision. The RFC's
`promoted-to` field lists all the resulting ADR filenames. No need to split
the RFC.

**Reviving a `withdrawn` RFC.** Write a new RFC that links back to the
withdrawn one in its `## More Information`. Do not reopen the withdrawn file;
it is part of the historical record.

## Lifetime

An RFC can live in this directory indefinitely. That is not a defect. If a
question is worth thinking about but not yet worth committing to, leaving the
RFC in `exploring` is the correct state. Only change the status when something
actually changes.

To keep the folder from quietly rotting, the RFC index gets a pass at each
bi-weekly NeIC SDA-Devs meet-up. Every `exploring` or `ready-for-decision`
RFC is touched, withdrawn, or left with a short note explaining why it is
still open. The review is a lightweight forcing function, not a deadline.

## How this relates to ADRs

| | `docs/rfcs/` | `docs/decisions/` |
| --- | --- | --- |
| Purpose | Exploration | Commitment |
| Typical question | *"Should we?"* | *"We did, because."* |
| Status vocabulary | `exploring`, `ready-for-decision`, `promoted`, `withdrawn` | `proposed`, `accepted`, `rejected`, `deprecated`, `superseded` |
| Template | MADR 4.0.0 structure, minus `## Decision Outcome` and `### Confirmation`, plus `## Open Questions` | Full MADR 4.0.0 |
| Expected PR lifetime | Indefinite | Short — merges when the discussion has already happened |

ADR statuses only apply after a file lives in `docs/decisions/`. An RFC is not
a `proposed` ADR; it is a separate artifact with its own lifecycle.

## Index

| # | RFC | Status | Promoted to |
| --- | --- | --- | --- |

*(empty at the time this folder is introduced; entries added as RFCs are
written)*
