---
status: exploring
date: "2026-09-02"
discussion: "https://github.com/neicnordic/sensitive-data-archive/pull/2263"
authors:
  - "@jhagberg"
consulted:
  - "@KarlG-nbis"
  - "@jbygdell"
  - "@kjellp"
  - "@viklund"
informed:
  - "@neicnordic/sensitive-data-development-collaboration"
---

# Standardize on `accessionID` everywhere?

## Context and Problem Statement

The SDA project uses multiple names for the same concept — archive identifiers:
`stableId`, `accessionId`, `accessionID`, `stable_id`, and variations. When
this RFC was first drafted (2026-03) Go code had a loose convention where
`stableID` appeared in database-layer functions and `AccessionID` in
API/message-facing code, while the database columns use `stable_id`. This
split is inconsistent, undocumented, and causes confusion during discussions
because the same information is referred to by different names depending on
the layer.

Since then the Go side has converged on its own: as of 2026-09-02 the `sda`
module has no `stableID` identifiers left, and the remaining ones live in the
legacy `sda-download` module (see the [survey](#codebase-survey)). What has
*not* changed is the database: the columns are still `stable_id`, and every
SQL string in Go, every legacy view and function, `sda-doa`, and a number of
integration and seed scripts still spell it that way.

Which single term should we adopt across the entire stack — Go application
code, API responses, and database schema? And is renaming the column even the
right move, or should the column go away?

Raised by @KarlG-nbis in PR #2232. Originally drafted as ADR-0001 in PR #2263;
converted to an RFC under [ADR-0005][adr-0005] because the rollout questions
below haven't converged yet.

## Decision Drivers

* **Consistency** — one name for one concept across the entire stack
* **Domain alignment** — use the term the bioinformatics community recognizes
* **Clarity in discussion** — avoid confusion when team members refer to the
  same data by different names
* **Grepability** — a single term makes it easy to find all usages
* **Cost and risk** — a DB column rename is a coordinated rollout across every
  service and site; the benefit has to justify that (raised by @kjellp)

## Considered Options

1. **`accessionID` everywhere** — rename Go code *and* DB columns to use the
   bioinformatics domain term
2. **`stableID` everywhere** — rename Go code *and* DB columns to match the
   current DB naming
3. **`accessionID` in Go only** — rename Go code but leave DB columns as
   `stable_id`
4. **Leave as-is** — both terms coexist
5. **Drop `stable_id`, use the reference tables** — remove the column from
   `sda.files` and `sda.datasets` and store accession IDs in the existing but
   unused `file_references` / `dataset_references` tables (proposed by
   @viklund)

## Open Questions

* **Rename or drop the column?** @viklund would rather get rid of the
  `stable_id` column in `files` and `datasets` altogether and use the
  `file_references` / `dataset_references` tables that already exist in the
  schema (option 5). @jbygdell noted this is v4.0 scope and should be coupled
  with removing the unused `local_ega` legacy tables. If the team wants to go
  that way, renaming the column first (option 1) is wasted work; if not,
  option 5 should be written down as rejected so it stops resurfacing. This
  is the question that blocks everything else.
* **Is option 3 enough as a first step?** @kjellp argues option 1 is the most
  costly and risk-prone choice, and that option 3 is a cheap stepping stone
  that makes the boundary predictable (`accessionID` in code, `stable_id` in
  the DB). The survey shows the `sda` module has effectively landed there
  already. Do we finish option 3 (rename the last `sda-download` and
  `sda-doa` identifiers, document the convention) and stop, or is the DB
  rename still wanted?
* **Which entity?** @kjellp: `accessionID` alone does not say whether it is a
  file, a dataset, or another (F)EGA entity, so some context is always needed
  regardless of option. Do we want a naming rule for that context (e.g.
  `fileAccessionID` / `datasetAccessionID` in code, `accession_id` plus the
  table name in SQL), or leave it to local judgement? Are there other names for
  the same thing hiding in the codebase, e.g. in `sda-doa`?
* **DB migration strategy.** The two rollouts under Implementation guidance —
  dual-name support during a transition window, or a coordinated maintenance
  window — both work on paper. Which fits how NeIC sites actually deploy
  changes? Has anyone running SDA expressed a preference?
* **Service-by-service sequencing.** Application PRs touch `sda` (Go),
  `sda-download` (Go), `sda-admin` (Go), and `sda-doa` (Java). Who coordinates
  across them? Is there an active `sda-doa` maintainer who can ship the Java
  rename in lockstep?
* **External consumers.** Inside this repo, integration tests, benchmark and
  dev-tools seed scripts, and two READMEs reference `stable_id` directly
  (15 files outside `postgresql/` and `*.go`). Are there monitoring queries,
  dashboards, or third-party integrations at NeIC sites that do the same? If
  so, the blast radius is wider than the SDA codebase alone.
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
* Good, because it is already the dominant name in the codebase (~350 Go
  identifiers vs 22 for `stableID`)
* Good, because it matches the v2.0.0 API spec — no API/client changes
  needed
* Good, because one name across all layers eliminates translation and
  discussion confusion
* Neutral, because `accessionID` alone is ambiguous — file, dataset, or other
  (F)EGA entity — so a minimum of context is needed whichever option is
  chosen (@kjellp)
* Bad, because it is the most costly and risk-prone option: a DB column rename
  is a coordinated rollout across every service and every site (@kjellp)
* Bad, because the DB side is large: 57 `stable_id` occurrences in
  `postgresql/` (schema and migrations), 110 more inside SQL strings in Go
  across 27 files, the `sda-doa` column mapping, and 15 integration and
  seed scripts
* Bad, because the DB migration touches columns, views, functions, and
  triggers — requires coordinated rollout with application deployments
* Bad, because if the team later chooses option 5 the column rename is
  throwaway work

### `stableID` everywhere

Rename Go code and API to use `stableID` / `stable_id` everywhere, matching
the current DB schema.

* Good, because it directly mirrors the current DB schema — zero DB migration
* Bad, because "stable" is vague and not a recognized bioinformatics term
* Bad, because the v2.0.0 API already uses `accessionID` — would be a
  breaking API change
* Bad, because it now means renaming ~350 Go identifiers back to the name the
  codebase has already moved away from

### `accessionID` in Go only

Rename Go application code but leave the DB columns as `stable_id`.

* Good, because no DB migration needed — significantly lower cost and risk
  than option 1 (@kjellp)
* Good, because it is a stepping stone toward option 1 — the code rename can
  land now and the DB rename can follow later if the team decides to
  (@kjellp)
* Good, because the boundary becomes predictable and well-defined:
  `accessionID` in code, `stable_id` in the DB, rather than mixed (@kjellp)
* Good, because the `sda` module is already there; what remains is 22
  identifiers in `sda-download`, one column mapping in `sda-doa`, and writing
  the convention down
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

### Drop `stable_id`, use the reference tables

Drop the `stable_id` column from `sda.files` and `sda.datasets`. Store
accession IDs (e.g. EGA file and dataset IDs) in the existing
`file_references` and `dataset_references` tables, keyed by
`reference_scheme`. The tables were created for external identifiers
(`initdb.d/01_main.sql`, migration `04.sql`) but no Go code reads or writes
them today.

* Good, because it removes the naming problem at the root — there is no
  column to rename
* Good, because it uses the reference tables for their intended purpose:
  external identifiers with a scheme, timestamps, and expiry
* Good, because it allows more than one reference scheme per file or dataset
  (EGA, DOI, …)
* Bad, because it is a much larger architectural change than a rename —
  v4.0 scope (@jbygdell)
* Bad, because the reference tables are completely unused today; every read
  and write path in every service has to be wired up to them
* Bad, because it affects every service that reads or writes `stable_id`
  (all Go services, `sda-doa`, message schemas, API contracts)
* Bad, because it should be coupled with removing the unused `local_ega`
  legacy tables to avoid a half-finished migration (@jbygdell)

## More Information

### Direction currently favoured

Option 1, `accessionID` everywhere, is still what the author and the original
reviewers (@KarlG-nbis, @jbygdell in the March thread) lean towards: it is the
established domain term, the code has already converged on it, and using the
same name in the DB removes the last translation step. Two objections are
live and are listed by name under [Open Questions](#open-questions): @kjellp's
that option 3 is the cheaper, sufficient first step, and @viklund's that the
column should be dropped rather than renamed (option 5). The first Open
Question has to be answered before this RFC can be promoted per
[ADR-0005][adr-0005].

If we go with option 1, the naming convention would be:

| Layer | Before | After |
| --- | --- | --- |
| Go application code | `accessionID` (22 `stableID` left in `sda-download`) | `accessionID` |
| Java application code (`sda-doa`) | `stableId` | `accessionId` |
| API responses | `accessionID` | `accessionID` (unchanged) |
| DB column names | `stable_id` | `accession_id` |
| DB views and functions (legacy layer) | `stable_id` | `accession_id` |
| DB query strings / scan targets | `stable_id` | `accession_id` |

### Codebase survey

Refreshed 2026-09-02 against `main`. The 2026-03-04 numbers are kept for
comparison; the drop in `stableID` identifiers is the result of ordinary
refactoring in the `sda` module since March, not of this RFC.

**Go identifiers (`*.go`):**

| Variant | 2026-03-04 | 2026-09-02 | Where (2026-09) |
| --- | --- | --- | --- |
| `accessionID` (camelCase) | ~92 | 183 | everywhere |
| `AccessionID` (PascalCase) | ~134 | 167 | everywhere |
| `stableID` (camelCase) | ~100 | 1 | one test string in `sda` |
| `StableID` (PascalCase) | ~34 | 21 | `sda-download/api/s3/s3.go`, `sda-download/internal/database/database.go` |

**`stable_id` column references:**

| Scope | Files | Occurrences |
| --- | --- | --- |
| Schema (`postgresql/initdb.d`) | 4 | 26 |
| Migrations (`postgresql/migratedb.d`) | 4 | 31 |
| SQL strings in Go (`*.go`, mostly `sda/cmd/download/database`) | 27 | 110 |
| Integration tests, benchmark and dev-tools seed scripts, READMEs | 15 | — |
| `sda-doa` (`Dataset.java`, `@Column(name = "stable_id")`) | 1 | 1 |

Affected SQL objects: `sda.files.stable_id`, `sda.datasets.stable_id`,
`sda.dataset_event_log` FK, views in `local_ega` and `local_ega_ebi` schemas,
functions/triggers (`main_insert`, `main_update`, `finalize_file`,
`filedataset_insert`).

Also relevant to option 5: `dataset_references` and `file_references` exist
in the schema and are not referenced from any Go code; the `local_ega*`
legacy schemas are still created by `initdb.d/03.*_legacy_*.sql`.

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

* `sda` (Go) — SQL strings in `sda/internal/database/` and
  `sda/cmd/download/database/`
* `sda-download` (Go) — the remaining `StableID` identifiers and SQL strings
  in `sda-download/internal/database/`, `sda-download/api/`
* `sda-admin` (Go)
* `sda-doa` (Java) — `Dataset.java` column mapping
* Integration tests and seed scripts under `.github/integration/` and
  `dev-tools/`

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

### Discussion history

* 2026-02: raised by @KarlG-nbis in PR #2232; ADR-0001 opened as PR #2263.
* 2026-02-23 to 2026-03-03 (PR #2263 review): @KarlG-nbis asked for the
  rename to go all the way down to the database so there is only one name for
  the information; @jbygdell agreed. The ADR was widened from "Go only" to
  options 1–4 above.
* 2026-03-23 (PR #2263 review): @kjellp raised the cost/risk of option 1, the
  file-vs-dataset ambiguity of `accessionID`, and option 3 as a stepping
  stone. Folded into the Pros and Cons and Open Questions.
* 2026-03-25 (Slack) and 2026-03-30 (PR #2263): @viklund proposed dropping
  the column in favour of `file_references` / `dataset_references`; @jbygdell
  noted v4.0 scope and coupling with `local_ega` removal. Added as option 5.
* 2026-05-20: converted from ADR-0001 to RFC-0001 per [ADR-0005][adr-0005].
* 2026-09-02: thread folded into the file per the
  [RFC review loop](README.md#how-to-write-an-rfc); codebase survey refreshed.

[adr-0005]: ../decisions/0005-introduce-rfcs-as-upstream-exploration-phase.md
