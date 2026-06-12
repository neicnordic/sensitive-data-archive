---
status: exploring
date: "2026-05-20"
authors:
  - "@neicnordic/sensitive-data-development-collaboration"
consulted: []
informed: []
---

# Standardize on `accessionID` everywhere?

## Context and Problem Statement

The SDA project uses multiple names for the same concept — archive identifiers:
`stableId`, `accessionId`, `accessionID`, `stable_id`, and variations. In Go
code there is a loose convention where `stableID` appears in database-layer
functions and `AccessionID` in API/message-facing code, while the database
columns use `stable_id`. This split is inconsistent, undocumented, and causes
confusion during discussions because the same information is referred to by
different names depending on the layer.

Which single term should we adopt across the entire stack — Go application
code, API responses, and database schema?

Raised by @KarlG-nbis in PR #2232. Originally drafted as ADR-0001 in PR #2263;
converted to an RFC under [ADR-0005][adr-0005] because the rollout questions
below haven't converged yet.

## Decision Drivers

* **Consistency** — one name for one concept across the entire stack
* **Domain alignment** — use the term the bioinformatics community recognizes
* **Clarity in discussion** — avoid confusion when team members refer to the
  same data by different names
* **Grepability** — a single term makes it easy to find all usages

## Considered Options

1. **`accessionID` everywhere** — rename Go code *and* DB columns to use the
   bioinformatics domain term
2. **`stableID` everywhere** — rename Go code *and* DB columns to match the
   current DB naming
3. **`accessionID` in Go only** — rename Go code but leave DB columns as
   `stable_id`
4. **Leave as-is** — both terms coexist

## Open Questions

* **DB migration strategy.** The two rollouts under Implementation guidance —
  dual-name support during a transition window, or a coordinated maintenance
  window — both work on paper. Which fits how NeIC sites actually deploy
  changes? Has anyone running SDA expressed a preference?
* **Service-by-service sequencing.** Application PRs touch `sda` (Go),
  `sda-download` (Go), `sda-admin` (Go), and `sda-doa` (Java). Who coordinates
  across them? Is there an active `sda-doa` maintainer who can ship the Java
  rename in lockstep?
* **External consumers.** Are there monitoring queries, dashboards, or
  third-party integrations that reference `stable_id` directly? If so, the
  blast radius is wider than the SDA codebase alone.
* **Reversibility window.** If dual-name support is chosen, how long does it
  stay before removal? Tied to deployment cadence across all NeIC sites.
* **Migration script placement.** A new `migratedb.d` script handles the
  rename — what migration number does it get, and does it depend on any
  other in-flight schema changes?

## Pros and Cons of the Options

### `accessionID` everywhere

Rename Go code, DB columns, and SQL queries to use `accessionID` /
`accession_id` consistently.

* Good, because "accession" is the standard bioinformatics term (EGA, ENA,
  dbGaP all use it)
* Good, because it is already the dominant name in the codebase (~230
  occurrences)
* Good, because it matches the v2.0.0 API spec — no API/client changes
  needed
* Good, because one name across all layers eliminates translation and
  discussion confusion
* Bad, because ~187 Go `stableID` occurrences, ~47 SQL `stable_id`
  occurrences (across 8 files), and `sda-doa` (Java) must be renamed
* Bad, because DB migration touches columns, views, functions, and
  triggers — requires coordinated rollout with application deployments

### `stableID` everywhere

Rename Go code and API to use `stableID` / `stable_id` everywhere, matching
the current DB schema.

* Good, because it directly mirrors the current DB schema — zero DB migration
* Bad, because "stable" is vague and not a recognized bioinformatics term
* Bad, because the v2.0.0 API already uses `accessionID` — would be a
  breaking API change
* Bad, because it is the minority name in the codebase (~187 vs ~230)

### `accessionID` in Go only

Rename Go application code but leave the DB columns as `stable_id`.

* Good, because no DB migration needed
* Neutral, because Go code becomes consistent within itself
* Bad, because the name mismatch between Go code and DB persists — the same
  information is still called different things in different layers
* Bad, because discussions still require context ("do you mean the column
  name or the Go name?")

### Leave as-is

Both `stableID` and `accessionID` coexist with the loose layering convention.

* Good, because zero effort — no rename, no risk of introducing bugs
* Bad, because the convention is undocumented and inconsistently followed
* Bad, because new contributors must learn which name to use where
* Bad, because grepping for all usages of the concept requires searching
  multiple terms

## More Information

### Direction currently favoured

Option 1, `accessionID` everywhere. It is the established domain term, it is
already the dominant name in the codebase (~230 occurrences vs ~187 for
`stableID`), and using the same name across all layers removes the translation
step that keeps tripping up discussion. The blockers are the rollout questions
in [Open Questions](#open-questions) above, not the naming itself. If those
questions land, this RFC is ready to be promoted to an ADR per
[ADR-0005][adr-0005].

If we go with option 1, the naming convention would be:

| Layer | Before | After |
| --- | --- | --- |
| Go application code | mixed `stableID` / `accessionID` | `accessionID` |
| Java application code (`sda-doa`) | `stableId` | `accessionId` |
| API responses | `accessionID` | `accessionID` (unchanged) |
| DB column names | `stable_id` | `accession_id` |
| DB views and functions (legacy layer) | `stable_id` | `accession_id` |
| DB query strings / scan targets | `stable_id` | `accession_id` |

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
functions/triggers (`main_insert`, `main_update`, `finalize_file`,
`filedataset_insert`).

**Java (`sda-doa`):** `Dataset.java` maps `@Column(name = "stable_id")`.

### Implementation guidance (if option 1 is chosen)

* No API change — the public API already uses `accessionID`.
* The rename is a coordinated rollout — the DB column rename and application
  code changes must be deployed together to avoid breaking running services.

**Two viable rollout strategies:**

1. **DB migration with dual-name support**: add the new `accession_id` column
   (or rename and create an alias/view) so that both old and new application
   code can coexist during the transition window.
2. **Coordinated maintenance window**: brief downtime where the DB rename and
   all services are updated together.

Which to pick is an open question — see above.

**Application PRs** (can be split per service for reviewable diffs):

* `sda` (Go) — `sda/internal/database/`, `sda/cmd/`
* `sda-download` (Go) — `sda-download/internal/database/`, `sda-download/api/`
* `sda-admin` (Go)
* `sda-doa` (Java) — `Dataset.java` column mapping

**Migration steps:**

1. Update `initdb.d` scripts so fresh installs use `accession_id` from the
   start.
2. Add a new migration script in `migratedb.d` for existing deployments:
   `ALTER TABLE sda.files RENAME COLUMN stable_id TO accession_id;` (and
   likewise for `sda.datasets`, plus update the FK, views, and functions).
3. Remove dual-name support (if used) once all services are confirmed
   deployed on the new name.
4. Historical migration scripts (`02.sql`–`04.sql`, `09.sql`) reference
   `stable_id` and should be left as-is — they represent the schema at the
   time they were written.

**Rollback**: if issues arise, reverse the column rename and redeploy the
previous application versions. `ALTER TABLE ... RENAME COLUMN` is reversible.

### Origin

Raised by @KarlG-nbis in PR #2232.

[adr-0005]: ../decisions/0005-introduce-rfcs-as-upstream-exploration-phase.md
