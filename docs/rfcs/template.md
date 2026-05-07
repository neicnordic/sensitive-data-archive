---
# RFC front matter. These are optional unless noted. Remove fields you do not need.
status: exploring # exploring | ready-for-decision | promoted | withdrawn
promoted-to: [] # one or more ADR filenames — set when status is `promoted`
date: "YYYY-MM-DD" # when the RFC was last updated
authors: [] # people actively shaping this RFC
consulted: [] # subject-matter experts consulted (two-way communication)
informed: [] # kept up to date on the discussion (one-way communication)
---

<!--
Promotion checklist (when this RFC becomes an ADR):

1. Create the ADR file at docs/decisions/NNNN-title-with-dashes.md using the
   next free ADR number. Start from docs/decisions/template.md. Pull over the
   Context, Considered Options, and the chosen option; drop the Open Questions
   that are resolved by the decision.
2. Fill in ## Decision Outcome with "Chosen option: X, because Y", and add
   ### Confirmation if applicable.
3. In this RFC file: flip status exploring | ready-for-decision -> promoted,
   set promoted-to to the new ADR filename (or filenames, if the RFC yielded
   more than one decision), and update the date.
4. Do not edit the body of this RFC after promotion. The RFC is frozen as a
   reference artifact for the exploration that led to the ADR.
5. Promotion is complete only once both indices are updated: the RFC row in
   docs/rfcs/README.md (status promoted, link to the new ADR) and the ADR row
   in docs/decisions/README.md.

See [ADR-0005](../decisions/0005-introduce-rfcs-as-upstream-exploration-phase.md)
for the full rationale.
-->

# {short title, representative of the question the RFC is exploring}

## Context and Problem Statement

{Describe the context and the problem in two to three sentences, or as a short
story. Link to collaboration boards, issues, or prior discussions. Unlike an
ADR, an RFC does not have to frame the problem as a question with an imminent
answer — it is fine to describe a tension the team wants to explore.}

<!-- This is an optional element. Feel free to remove. -->
## Decision Drivers

* {driver 1 — a force or concern the RFC should account for}
* {driver 2}
* …

## Considered Options

* {title of option 1}
* {title of option 2}
* {title of option 3}
* …

<!--
Intentionally no `## Decision Outcome` section in RFCs. The whole point of an
RFC is that the team is not yet ready to commit. Add this section at promotion
time.
-->

## Open Questions

{List what the team does not yet know or agree on. Unresolved items are not a
gap in the RFC; they are the point of writing one. When the list is short
enough that the team can honestly write "Chosen option: X, because Y", the
RFC is ready for promotion.}

* {open question 1}
* {open question 2}
* …

<!-- This is an optional element. Feel free to remove. -->
## Pros and Cons of the Options

### {title of option 1}

<!-- This is an optional element. Feel free to remove. -->
{example | description | pointer to more information | …}

* Good, because {argument a}
* Good, because {argument b}
<!-- use "neutral" if the given argument weights neither for good nor bad -->
* Neutral, because {argument c}
* Bad, because {argument d}
* …

### {title of other option}

{example | description | pointer to more information | …}

* Good, because {argument a}
* Good, because {argument b}
* Neutral, because {argument c}
* Bad, because {argument d}
* …

<!-- This is an optional element. Feel free to remove. -->
## More Information

{Provide additional evidence or context. Link to related RFCs, ADRs, prior art
from other projects, and any external discussion threads. If the RFC has a
suspected eventual decision direction, note it here — but do not put a
commitment in the `## Decision Outcome` section until promotion.}
