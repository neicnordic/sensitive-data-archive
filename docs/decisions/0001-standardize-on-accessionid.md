---
status: proposed
date: 2026-02-23
decision-makers:
  - "@neicnordic/sensitive-data-development-collaboration"
---

# Standardize on `accessionID` over `stableId` in Go codebase

## Context and Problem Statement

The Go codebase uses multiple names for archive identifiers: `stableId`, `accessionId`,
`accessionID`, `stable_id`, and variations. While there is a loose convention where
`stableID` appears in database-layer functions and `AccessionID` in API/message-facing
code, this layering is inconsistent and undocumented — many call sites mix the terms
freely. The result is code that is harder to read, grep, and reason about.

Which single term should we use in application-level Go code?

Raised by @KarlG-nbis in PR #2232.

## Decision Drivers

* **Consistency** — one name for one concept across the codebase
* **Domain alignment** — use the term the bioinformatics community recognizes
* **Minimal disruption** — avoid DB migrations or API breaking changes
* **Grepability** — a single term makes it easy to find all usages

## Considered Options

1. **`accessionID`** — bioinformatics domain term (EGA accession IDs: `EGAD*`, `EGAF*`)
2. **`stableID`** — matches the existing DB column name `stable_id`
3. **Leave as-is** — both terms coexist

## Decision Outcome

Chosen option: **`accessionID`**, because it is the established domain term, already
dominant in the codebase (~232 occurrences vs ~187 for `stableID`), and communicates
meaning more clearly ("accession" is well understood in bioinformatics, "stable" is vague).

### Boundary rules

| Layer | Name | Rationale |
| --- | --- | --- |
| Go application code | `accessionID` | Single consistent term |
| API responses | `accessionID` | Already the case in v2.0.0 spec |
| DB column names | `stable_id` (unchanged) | Renaming columns is painful and a different naming context |
| DB query strings / scan targets | `stable_id` | Matches the DB schema at the Go↔DB boundary |

### Consequences

* Good, because one term to grep for; easier onboarding; matches domain language.
* Good, because no DB migration, no API change — purely internal rename.
* Neutral, because requires focused rename PRs across the Go services.
* Bad, because churn in diffs; mitigated by splitting into per-service PRs.

### Confirmation

All Go code (excluding DB query strings) uses `accessionID` and tests pass.
A grep for `stableID` / `stableId` in `*.go` files returns zero matches outside of
SQL string literals.

## Pros and Cons of the Options

### `accessionID`

Use `accessionID` everywhere in Go application code; keep `stable_id` only in DB query strings.

* Good, because "accession" is the standard bioinformatics term (EGA, ENA, dbGaP all use it)
* Good, because it is already the dominant name in the codebase (~232 occurrences)
* Good, because it matches the v2.0.0 API spec — no API/client changes needed
* Good, because callers no longer need to know the DB column name
* Neutral, because the DB-layer functions that currently use `stableID` will be renamed, breaking local conventions
* Bad, because ~187 `stableID` occurrences must be renamed across multiple services

### `stableID`

Use `stableID` everywhere in Go code to match the DB column `stable_id`.

* Good, because it directly mirrors the DB schema — no mental translation at the DB boundary
* Bad, because "stable" is vague and not a recognized bioinformatics term
* Bad, because the v2.0.0 API already uses `accessionID` — would require an API change or perpetuate the split
* Bad, because it is the minority name in the codebase (~187 vs ~232)

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

* This is a naming-only change — no behavior change, no API change, no DB migration.
* Split the rename into per-service PRs (sda, sda-download, sda-admin) to keep diffs reviewable.
* The `stable_id` DB column may be renamed in a future migration if the team decides
  to align the DB layer as well, but that is out of scope for this decision.

### Origin

Raised by @KarlG-nbis in PR #2232.
