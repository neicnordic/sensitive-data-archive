---
status: proposed
date: "2026-09-07"
decision-makers:
  - "@neicnordic/sensitive-data-development-collaboration"
consulted:
  - "@KarlG-nbis"
  - "@jbygdell"
  - "@kjellp"
  - "@viklund"
informed: []
---

# Use `accessionID` in application code and keep `stable_id` as the database column

## Context and Problem Statement

The archive identifier of a file or dataset goes by `stableID`,
`accessionID` and `stable_id` in different layers of the codebase, and no
written rule says which name belongs where. [RFC-0001][rfc-0001] explored the
question and holds the pros and cons, the codebase survey and the discussion
history, so this record does not repeat them. By September 2026 ordinary
refactoring had removed every `stableID` identifier from the `sda` module.
What remained was a handful of identifiers in `sda-download` and `sda-doa`,
and no written convention. Which name do we commit to, and in which layers?

## Decision Drivers

* One name per layer, written down, so reviews stop relitigating it
* No database migration for a naming change (cost and risk, @kjellp)
* Do not pre-empt the v4.0 schema work, where the column may be dropped
  rather than renamed (@viklund, @jbygdell)

## Considered Options

1. `accessionID` everywhere, including a column rename
2. `stableID` everywhere
3. `accessionID` in application code only; the column stays `stable_id`
4. Leave as-is
5. Drop `stable_id` and use the `file_references` / `dataset_references`
   tables

See [RFC-0001][rfc-0001] for the pros and cons of each option.

## Decision Outcome

Chosen option: "`accessionID` in application code only", because the code is
already there, a column rename would need a coordinated migration at every
site without changing any behaviour, and whether the column is renamed or
dropped is a v4.0 question.

| Layer | Name |
| --- | --- |
| Go and Java application code | `accessionID` / `accessionId` |
| API responses and messages | `accessionID` (unchanged) |
| Database columns, views, functions and SQL strings | `stable_id` (unchanged) |

Option 5 is deferred rather than rejected. We reopen it as a new RFC together
with the removal of the `local_ega` legacy schema. The open questions in
RFC-0001 about migration strategy and an entity-qualified naming rule move to
that RFC.

### Consequences

* Good, because the rule is easy to apply: `accessionID` in code, `stable_id`
  in SQL
* Good, because there is no migration, and so no maintenance window or
  dual-name transition period
* Neutral, because the surrounding type or table still has to say whether an
  `accessionID` belongs to a file or a dataset. This record adds no naming
  rule for that.
* Bad, because the name still changes between code and SQL, so a conversation
  about the column still needs the word "column"

### Confirmation

* `grep -rn --include='*.go' -e stableID -e StableID .` returns nothing once
  the follow-up code PR has landed
* In `sda-doa`, the entity field is `accessionId` while the
  `@Column(name = "stable_id")` mapping is unchanged

## More Information

The team decided this at the NeIC SDA-Devs meet-up on 2026-09-07. RFC-0001
is `promoted` with this file in its `promoted-to`. The RFC body is frozen, so
any revision of this decision goes in this file.

[rfc-0001]: ../rfcs/0001-standardize-on-accessionid.md
