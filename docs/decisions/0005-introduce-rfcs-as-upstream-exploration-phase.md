---
status: proposed
date: "2026-04-20"
decision-makers:
  - "@neicnordic/sensitive-data-development-collaboration"
consulted: []
informed: []
---

# Introduce RFCs as an Upstream Exploration Phase for ADRs

## Context and Problem Statement

The decision log in `docs/decisions/` is intended to record what the system is
committed to. In practice, some proposals open an ADR PR long before the team
can honestly write *"Chosen option: X, because Y"*. The discussion lingers, the
PR stays open, and the decision log ends up mixing committed decisions with
speculative futures. This weakens its value as a trustworthy record of what the
system actually does and intends to do.

A concrete example: at the time of writing, two ADR PRs
([#2263 ADR-0001][pr-2263], [#2320 ADR-0004][pr-2320]) have been open for
extended periods without convergence. Their implementation horizon is unclear.
They belong to a different conversation than decisions we are ready to commit
to.

We need a place for exploration that does not add noise to the decision log.

## Decision Drivers

* **Decision log trustworthiness** — entries in `docs/decisions/` should reflect
  actual commitments, not hopes.
* **Patience for open questions** — exploration needs a venue more patient
  than a PR that is expected to merge.
* **Low ceremony** — any new process must be lightweight or it will be ignored.
* **Ease of promotion** — turning a mature exploration into a decision record
  should be a near-trivial edit, so the separation does not penalise good
  work.
* **No semantic drift** — the existing status vocabulary
  (`proposed` → `accepted` \| `rejected` \| `deprecated` \| `superseded`) is
  working and should not be redefined.

## Considered Options

1. **Keep everything in `docs/decisions/`** — do nothing; accept that some ADRs
   will linger in `proposed`.
2. **Parking-lot tag on ADRs** — add a label or front-matter flag (e.g.
   `horizon: distant`) to distinguish speculative ADRs from imminent ones.
3. **Introduce `docs/rfcs/` as an upstream exploration phase** — RFCs live
   outside the decision log, are promoted to ADRs when the team can commit.

## Decision Outcome

Chosen option: **option 3, introduce `docs/rfcs/` as an upstream exploration
phase**. It matches the separation that Rust, Python, Kubernetes, and Go
already use between *"should we?"* and *"we did, because"*. Exploration gets
its own venue, and the existing ADR status vocabulary stays as it is.

### Consequences

* **Good**: The decision log reflects real commitments only; `proposed` keeps
  its current meaning of *"under discussion in a PR, expected to merge"*.
* **Good**: Exploration has a patient venue. An RFC can live in the repository
  indefinitely without cluttering the decision log.
* **Good**: Promotion is a light edit — copy the relevant content into a new
  ADR file, fill in `## Decision Outcome`, and freeze the RFC. The RFC template
  is a strict subset of MADR, so nothing has to be restructured on the way
  across.
* **Good**: The RFC file stays in `docs/rfcs/` after promotion. The exploration
  remains a git artifact in its own right, instead of living only in a GitHub
  PR thread.
* **Neutral**: Contributors must learn the difference between an RFC and an
  ADR. The README files and the promotion rule make this explicit.
* **Bad**: An extra directory in `docs/` to explain. Mitigated by clear README
  text and a shared template.
* **Bad**: Risk that RFCs become a *dumping ground* that nobody promotes.
  Three things push back on that: an explicit promotion trigger (*"when you
  can write 'Chosen option: X, because Y' with a straight face"*), the
  expectation that ADR PRs opened from a mature RFC merge quickly, and a
  pass over the RFC index at each bi-weekly NeIC SDA-Devs meet-up — every
  open RFC either gets touched, withdrawn, or annotated with a reason it is
  still open.

### Confirmation

Confirmation is delivered in two steps:

1. The merge of this PR creates `docs/rfcs/` with its README and template, and
   adds the promotion rule to `docs/decisions/README.md`.
2. Two follow-up PRs close the currently open ADR PRs
   ([#2263][pr-2263], [#2320][pr-2320]) and re-open their content as RFCs.
   Successful completion of those follow-ups confirms that the flow works in
   practice.

## Pros and Cons of the Options

### Option 1 — Keep everything in `docs/decisions/`

* Good, because there is zero process change.
* Bad, because the decision log continues to mix commitments with speculation.
* Bad, because there is no signal for *"we are exploring this, do not expect a
  commitment yet"*.

### Option 2 — Parking-lot tag on ADRs

* Good, because it is a one-line change to existing ADRs.
* Bad, because it overloads the decision log with documents that are not
  decisions.
* Bad, because it introduces a second orthogonal lifecycle (horizon) on top of
  the existing status lifecycle, which is more semantic surface area than
  option 3.

### Option 3 — Introduce `docs/rfcs/` as an upstream exploration phase

* Good, because it matches the separation used by Rust
  ([rust-lang/rfcs][rust-rfcs]), Python ([PEP 1][pep-1]), Kubernetes
  ([KEP process][kep]), and Go ([golang/proposal][go-proposal]).
* Good, because the RFC template is a subset of MADR, so promotion is
  near-trivial.
* Good, because it leaves the existing ADR status lifecycle untouched.
* Neutral, because it introduces one new directory.
* Bad, because it requires a small amount of new documentation (two READMEs
  and a template).

## More Information

### Conventions Established by This ADR

| Convention | Detail |
| --- | --- |
| Directory | `docs/rfcs/` |
| File naming | `NNNN-title-with-dashes.md` — same as ADRs, numbering independent |
| RFC numbering | Starts at 0001; numbers are never reused |
| RFC status lifecycle | `exploring` → `ready-for-decision` → `promoted` \| `withdrawn` |
| RFC template | `docs/rfcs/template.md` (MADR 4.0.0 minus `## Decision Outcome` and `### Confirmation`, plus `## Open Questions`) |
| Promotion trigger | The team can honestly write *"Chosen option: X, because Y"* |
| Promotion mechanics | Create a new ADR file under the next free ADR number from [`docs/decisions/template.md`](template.md), pulling across the relevant content from the RFC. In the RFC: flip `status` to `promoted`, set `promoted-to` to the ADR filename(s), freeze the body. Promotion is only complete once the index in [this README](README.md#index) and the RFC index are both updated. |
| One RFC, multiple ADRs | Supported: `promoted-to` accepts a list of ADR filenames. |
| Reserved ADR numbers | `0001` and `0004` are reserved by open ADR PRs ([#2263][pr-2263], [#2320][pr-2320]). The next free ADR number is `0005` (this ADR); the number after is `0006`. |
| RFC lifetime | Unlimited. An RFC living for a long time without promotion is not a defect. Stale RFCs are surfaced at the bi-weekly NeIC SDA-Devs meet-up. |
| Post-promotion edits | A `promoted` or `withdrawn` RFC is frozen — only metadata and index rows change. Revisions to a decision live in the ADR. |
| ADR lifecycle unchanged | The ADR status lifecycle (`proposed` → `accepted` \| `rejected` \| `deprecated` \| `superseded`) is **not** modified by this ADR. ADR statuses only apply to files in `docs/decisions/`. |

### Relationship to other ADRs

This ADR adds to [ADR-0000][adr-0000]; it does not replace it. ADR-0000
established ADRs; this one adds an exploration phase upstream of them.

### References

* [MADR 4.0.0 template][madr] — the ADR template this project uses.
* [Rust RFC process][rust-rfcs] — canonical single-document RFC flow.
* [Python PEP 1][pep-1] — PEP status lifecycle and exploration conventions.
* [Kubernetes KEP process][kep] — provisional vs implementable states.
* [Go proposal template][go-proposal] — proposal-to-design-doc flow.
* [Candost Dagdeviren, *ADRs and RFCs*][candost] — an accepted RFC may spawn
  one or more ADRs.

[pr-2263]: https://github.com/neicnordic/sensitive-data-archive/pull/2263
[pr-2320]: https://github.com/neicnordic/sensitive-data-archive/pull/2320
[adr-0000]: 0000-use-markdown-architectural-decision-records.md
[madr]: https://github.com/adr/madr/blob/4.0.0/template/adr-template.md
[rust-rfcs]: https://github.com/rust-lang/rfcs/blob/master/0000-template.md
[pep-1]: https://peps.python.org/pep-0001/
[kep]: https://github.com/kubernetes/enhancements/tree/master/keps/NNNN-kep-template
[go-proposal]: https://github.com/golang/proposal/blob/master/design/TEMPLATE.md
[candost]: https://candost.blog/adrs-rfcs-differences-when-which/
