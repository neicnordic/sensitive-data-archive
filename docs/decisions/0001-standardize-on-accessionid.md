---
status: proposed
date: 2026-02-23
decision-makers:
  - "@neicnordic/sensitive-data-development-collaboration"
---

# Standardize on `accessionID` everywhere — Go code and database

## Context and Problem Statement

The SDA project uses multiple names for the same concept — archive identifiers: `stableId`,
`accessionId`, `accessionID`, `stable_id`, and variations. In Go code there is a loose
convention where `stableID` appears in database-layer functions and `AccessionID` in
API/message-facing code, while the database columns use `stable_id`. This split is
inconsistent, undocumented, and causes confusion during discussions because the same
information is referred to by different names depending on the layer.

Which single term should we adopt across the entire stack — Go application code, API
responses, and database schema?

Raised by @KarlG-nbis in PR #2232.

## Decision Drivers

* **Consistency** — one name for one concept across the entire stack
* **Domain alignment** — use the term the bioinformatics community recognizes
* **Clarity in discussion** — avoid confusion when team members refer to the same data by different names
* **Grepability** — a single term makes it easy to find all usages

## Considered Options

1. **`accessionID` everywhere** — rename Go code *and* DB columns to use the bioinformatics domain term
2. **`stableID` everywhere** — rename Go code *and* DB columns to match the current DB naming
3. **`accessionID` in Go only** — rename Go code but leave DB columns as `stable_id`
4. **Leave as-is** — both terms coexist

## Decision Outcome

Chosen option: **`accessionID` everywhere**, because it is the established domain term,
already dominant in the codebase (~232 occurrences vs ~187 for `stableID`), communicates
meaning more clearly ("accession" is well understood in bioinformatics, "stable" is vague),
and using the same name across all layers eliminates confusion during discussions.

### Naming convention

| Layer | Before | After |
| --- | --- | --- |
| Go application code | mixed `stableID` / `accessionID` | `accessionID` |
| API responses | `accessionID` | `accessionID` (unchanged) |
| DB column names | `stable_id` | `accession_id` |
| DB query strings / scan targets | `stable_id` | `accession_id` |

### Consequences

* Good, because one term across the entire stack — no translation between layers.
* Good, because discussions, code, and schema all use the same language.
* Good, because domain-aligned naming helps onboarding and external communication.
* Neutral, because requires focused rename PRs across the Go services.
* Bad, because a DB migration is needed to rename `stable_id` → `accession_id` in
  `sda.files` and `sda.datasets` (plus the FK in `sda.dataset_event_log`).
* Bad, because churn in diffs; mitigated by splitting into per-service PRs.

### Confirmation

* A grep for `stableID` / `stableId` / `stable_id` across Go code and SQL returns
  zero matches.
* DB migration runs cleanly and all integration tests pass.

## Pros and Cons of the Options

### `accessionID` everywhere

Rename Go code, DB columns, and SQL queries to use `accessionID` / `accession_id` consistently.

* Good, because "accession" is the standard bioinformatics term (EGA, ENA, dbGaP all use it)
* Good, because it is already the dominant name in the codebase (~232 occurrences)
* Good, because it matches the v2.0.0 API spec — no API/client changes needed
* Good, because one name across all layers eliminates translation and discussion confusion
* Bad, because ~187 Go `stableID` occurrences and ~53 SQL `stable_id` occurrences must be renamed
* Bad, because a DB migration is required (`files.stable_id`, `datasets.stable_id`, FK in `dataset_event_log`)

### `stableID` everywhere

Rename Go code and API to use `stableID` / `stable_id` everywhere, matching the current DB schema.

* Good, because it directly mirrors the current DB schema — zero DB migration
* Bad, because "stable" is vague and not a recognized bioinformatics term
* Bad, because the v2.0.0 API already uses `accessionID` — would be a breaking API change
* Bad, because it is the minority name in the codebase (~187 vs ~232)

### `accessionID` in Go only

Rename Go application code but leave the DB columns as `stable_id`.

* Good, because no DB migration needed
* Neutral, because Go code becomes consistent within itself
* Bad, because the name mismatch between Go code and DB persists — the same information
  is still called different things in different layers
* Bad, because discussions still require context ("do you mean the column name or the Go name?")

### Leave as-is

Both `stableID` and `accessionID` coexist with the loose layering convention.

* Good, because zero effort — no rename, no risk of introducing bugs
* Bad, because the convention is undocumented and inconsistently followed
* Bad, because new contributors must learn which name to use where
* Bad, because grepping for all usages of the concept requires searching multiple terms

## More Information

### Codebase survey (2026-02-23)

| Variant | Approx. count | Typical context |
| --- | --- | --- |
| `accessionID` (camelCase) | 91 | Variable names, function parameters |
| `AccessionID` (PascalCase) | 139 | Struct fields, function names |
| `stableID` (camelCase) | 100 | Variable names, function parameters |
| `StableID` (PascalCase) | 34 | Function names, comments |
| `stable_id` (snake_case) | 53 | SQL queries, DB column references |

### Implementation guidance

* No behavior change, no API change — purely a naming rename.
* **DB migration**: rename `stable_id` → `accession_id` in `sda.files`, `sda.datasets`,
  and update the FK in `sda.dataset_event_log`. Use `ALTER TABLE ... RENAME COLUMN`.
* **Go code**: split the rename into per-service PRs (sda, sda-download, sda-admin) to
  keep diffs reviewable. Update SQL query strings to use `accession_id` in the same PRs.
* The DB migration should land first so that Go code changes can target the new column name.

### Origin

Raised by @KarlG-nbis in PR #2232.
