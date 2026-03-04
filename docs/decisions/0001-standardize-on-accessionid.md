---
status: proposed
date: 2026-03-04
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
already dominant in the codebase (~230 occurrences vs ~187 for `stableID`), communicates
meaning more clearly ("accession" is well understood in bioinformatics, "stable" is vague),
and using the same name across all layers eliminates confusion during discussions.

### Naming convention

| Layer | Before | After |
| --- | --- | --- |
| Go application code | mixed `stableID` / `accessionID` | `accessionID` |
| Java application code (`sda-doa`) | `stableId` | `accessionId` |
| API responses | `accessionID` | `accessionID` (unchanged) |
| DB column names | `stable_id` | `accession_id` |
| DB views and functions (legacy layer) | `stable_id` | `accession_id` |
| DB query strings / scan targets | `stable_id` | `accession_id` |

### Consequences

* Good, because one term across the entire stack — no translation between layers.
* Good, because discussions, code, and schema all use the same language.
* Good, because domain-aligned naming helps onboarding and external communication.
* Neutral, because requires focused rename PRs across Go services and `sda-doa` (Java).
* Bad, because a DB migration is needed: `stable_id` appears 47 times across 8 SQL files —
  columns in `sda.files` and `sda.datasets`, FK in `sda.dataset_event_log`, plus legacy
  views (`local_ega.main`, `local_ega.files`, `local_ega.archive_files`,
  `local_ega_ebi.file`, `local_ega_ebi.filedataset`, `local_ega_ebi.file_dataset`,
  `local_ega_ebi.file_index_file`) and functions/triggers (`main_insert`, `main_update`,
  `finalize_file`, `filedataset_insert`).
* Bad, because the rename requires a coordinated rollout — running services issue
  `stable_id` queries, so a naive DB-first rename breaks them until the application
  code is also deployed (see implementation guidance below).
* Bad, because churn in diffs; mitigated by splitting into per-service PRs.

### Confirmation

* A grep for `stableID` / `stableId` / `stable_id` across Go code, Java code, and SQL
  (excluding historical migration scripts) returns zero matches.
* DB migration runs cleanly against both fresh installs (`initdb.d`) and upgrades
  (`migratedb.d`).
* All integration tests pass with the renamed columns and no service uses the old name.

## Pros and Cons of the Options

### `accessionID` everywhere

Rename Go code, DB columns, and SQL queries to use `accessionID` / `accession_id` consistently.

* Good, because "accession" is the standard bioinformatics term (EGA, ENA, dbGaP all use it)
* Good, because it is already the dominant name in the codebase (~230 occurrences)
* Good, because it matches the v2.0.0 API spec — no API/client changes needed
* Good, because one name across all layers eliminates translation and discussion confusion
* Bad, because ~187 Go `stableID` occurrences, ~47 SQL `stable_id` occurrences (across 8 files),
  and `sda-doa` (Java) must be renamed
* Bad, because DB migration touches columns, views, functions, and triggers — requires
  coordinated rollout with application deployments

### `stableID` everywhere

Rename Go code and API to use `stableID` / `stable_id` everywhere, matching the current DB schema.

* Good, because it directly mirrors the current DB schema — zero DB migration
* Bad, because "stable" is vague and not a recognized bioinformatics term
* Bad, because the v2.0.0 API already uses `accessionID` — would be a breaking API change
* Bad, because it is the minority name in the codebase (~187 vs ~230)

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

### Codebase survey (2026-03-04)

**Go code (`*.go`):**

| Variant | Count | Typical context |
| --- | --- | --- |
| `accessionID` (camelCase) | ~92 | Variable names, function parameters |
| `AccessionID` (PascalCase) | ~134 | Struct fields, function names |
| `stableID` (camelCase) | ~100 | Variable names, function parameters |
| `StableID` (PascalCase) | ~34 | Function names, comments |

**SQL (`postgresql/`):**

| Scope | Files | Occurrences |
| --- | --- | --- |
| Schema (`initdb.d`) | 4 | 21 |
| Migrations (`migratedb.d`) | 4 | 26 |
| **Total** | **8** | **47** |

Affected SQL objects: `sda.files.stable_id`, `sda.datasets.stable_id`,
`sda.dataset_event_log` FK, views in `local_ega` and `local_ega_ebi` schemas,
functions/triggers (`main_insert`, `main_update`, `finalize_file`, `filedataset_insert`).

**Java (`sda-doa`):** `Dataset.java` maps `@Column(name = "stable_id")`.

### Implementation guidance

* No API change — the public API already uses `accessionID`.
* The rename is a **coordinated rollout** — the DB column rename and application code
  changes must be deployed together to avoid breaking running services.

**Recommended rollout strategy:**

1. **DB migration with dual-name support**: add the new `accession_id` column (or rename
   and create an alias/view) so that both old and new application code can coexist during
   the transition window. Alternatively, deploy with a brief maintenance window where DB
   rename and all services are updated together.
2. **Application PRs** (can be split per service for reviewable diffs):
   * `sda` (Go) — `sda/internal/database/`, `sda/cmd/`
   * `sda-download` (Go) — `sda-download/internal/database/`, `sda-download/api/`
   * `sda-admin` (Go)
   * `sda-doa` (Java) — `Dataset.java` column mapping
3. **Update `initdb.d` scripts** so fresh installs use `accession_id` from the start.
4. **Add a new migration script in `migratedb.d`** for existing deployments:
   `ALTER TABLE sda.files RENAME COLUMN stable_id TO accession_id;` (and likewise for
   `sda.datasets`, plus update the FK, views, and functions).
5. **Remove dual-name support** once all services are confirmed deployed on the new name.
6. Historical migration scripts (`02.sql`–`04.sql`, `09.sql`) reference `stable_id` and
   should be left as-is — they represent the schema at the time they were written.

**Rollback**: if issues arise, reverse the column rename and redeploy the previous
application versions. The `ALTER TABLE ... RENAME COLUMN` is reversible.

### Origin

Raised by @KarlG-nbis in PR #2232.
