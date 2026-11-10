---
status: proposed
date: "2026-11-03"
decision-makers:
  - "@neicnordic/sensitive-data-development-collaboration"
consulted:
  - "@kjellp"
  - "@yasinmiran"
---

# Keep grants in the schema migration that creates the need

## Context and Problem Statement

The database schema is defined twice.
`postgresql/initdb.d/` builds a fresh database at the newest schema version, with `04_grants.sql` creating the 13 service users and roles and holding 137 `GRANT` lines.
`postgresql/migratedb.d/` brings an existing database from an older version to the newest one.
The entrypoint only runs `initdb.d` when the data directory is empty: once `$PGDATA/PG_VERSION` exists it runs the `migratedb.d` scripts until none reports `Doing migration from`, sets the `lega_in` and `lega_out` passwords and writes `pg_hba.conf`.
A line added to `04_grants.sql` therefore never reaches a database that was initialised by an earlier release.

Until now the two have been kept in step by habit.
Every migration from 02 to 22 that creates a table or a role also grants on it in the same file (22, for example, creates `sda.file_headers_backup` and grants `INSERT, SELECT, UPDATE` on it to `rotatekey`).
Migrations 23 to 28 add columns, indexes, a row and functions, which need no new grant: columns and indexes are covered by the table's grants, and new functions are executable by `PUBLIC` by default.
The habit was never written down, and two open pull requests show it slipping:

* #2717 adds `GRANT SELECT ON sda.dataset_references TO download;` to `04_grants.sql` only.
  The table exists on every database (it is created in `01_main.sql` and in migration 04), so an upgraded database has the table and lacks the grant.
  Two production databases have received it by hand.
* #2676 adds `mapper` grants to `04_grants.sql`.
  Its first version had no matching grants in its migration script; the current version has.

The failure shows up after an upgrade as a permission error in a service such as `sda-doa` or `mapper`, which reads like a bug in the service.
The sharpest case is a new table created in a migration whose grant lives only in `04_grants.sql`: a fresh database works, an upgraded one does not.

CI already builds both kinds of database side by side.
`.github/integration/postgres.yml` starts a `postgres` service from an empty volume and a `migrate` service on a volume that the `migrate_seed` service first fills with the schema version 0 fixture (through `seed_migrate_db.sh`), so the entrypoint migrates it to the newest version.
`10_sanity_check.sh` checks that `migrate` started from the fixture and reached the newest version.
The per-role query scripts (`20_inbox_queries.sh` to `70_download_queries.sh`) run against both services, but only cover the queries someone has written a test for.
Nothing compares the privileges of the two databases.

How should the project keep the grants of a migrated database identical to those of a fresh one?

## Decision Drivers

* A database upgraded from any earlier release ends up with the same privileges as a fresh one.
* A missing grant fails in CI, not in a service after deployment.
* Correctness does not depend on operators reading release notes or applying SQL by hand.
* Little new machinery: the migration series, the entrypoint and the CI compose file already exist.

## Considered Options

* Keep grants in the schema migration that creates the need, and check the end result.
* Maintain grants in a separate migration series next to the schema migrations.
* Keep only schema changes in migrations and leave grants to database administrators.
* Make `04_grants.sql` replayable and run it on every start.

## Decision Outcome

Chosen option: "Keep grants in the schema migration that creates the need, and check the end result", because it writes down the practice migrations 02 to 22 already follow, needs no change to the entrypoint, and the CI check catches the case where it is forgotten.

* A migration that creates a table, sequence, function or role also grants the privileges the services need on it, in the same file.
* A change to the privileges on an existing object, such as #2717, gets its own migration script with a version bump, even when it contains nothing but `GRANT` or `REVOKE` statements.
* `04_grants.sql` stays the fresh-database picture of the same end state: every grant a migration adds is also added there, in the same pull request.
* #2717 needs a migration script under this decision: the grant is on a table every database already has, so a line in `04_grants.sql` alone never reaches an upgraded one.
* Making `04_grants.sql` replayable stays open as a later addition; it does not conflict with this decision.

### Consequences

* Good, because a pull request that adds a grant carries both the fresh and the upgraded path, and review sees them together.
* Good, because the CI comparison turns "migrated and fresh databases have the same privileges" from a habit into a test.
* Bad, because every grant is still written twice, once in a migration and once in `04_grants.sql`.
  The CI check makes a mismatch fail rather than removing the duplication.
* Bad, because a grant-only change costs a schema version bump.

### Confirmation

* A new script in `.github/integration/tests/postgres/` dumps, on both the `migrate` and the `postgres` service, the privileges on every table, view, sequence and function in the `sda`, `local_ega`, `local_ega_download` and `local_ega_ebi` schemas and on the schemas themselves (from `pg_class.relacl`, `pg_proc.proacl` and `pg_namespace.nspacl`, expanded with `aclexplode`), together with role memberships from `pg_auth_members`.
  It sorts both lists and fails on any difference, printing the lines that differ.
  Role attributes are left out, because the `credentials` service sets `LOGIN` on both databases.
* The check is expected to fail on its first run.
  The schema version 0 fixture grants privileges on the legacy `local_ega` tables directly to `lega_in` and `lega_out`, and migrations 02 and 03 add bulk `GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA` statements, while a fresh database gives the same users their rights through role membership.
  The pull request that adds the check either aligns the two with a catch-up migration or lists each remaining difference in the test as a commented exception, so that every known deviation is written down and reviewed.
* `postgresql/README.md` gets a short section stating the rule above.
* A CI step fails a pull request that changes `postgresql/initdb.d/04_grants.sql` without also changing a file in `postgresql/migratedb.d/`.
  The privilege comparison is the real safety net; this step makes the omission visible in the pull request itself.

## Pros and Cons of the Options

### Keep grants in the schema migration that creates the need, and check the end result

* Good, because it is what migrations 02 to 22 already do, so no existing script changes.
* Good, because one migration file describes one complete step: the object and who may use it.
* Good, because the entrypoint stays as it is.
* Bad, because the grant is duplicated between the migration and `04_grants.sql`.
* Bad, because without the CI check it relies on the same habit that has already slipped.

### Maintain grants in a separate migration series next to the schema migrations

Schema changes stay in `migratedb.d/`, and grant changes go to a second numbered series with its own version table, run after the schema migrations.

* Good, because privileges have a clear home, and a grant-only change does not bump the schema version.
* Bad, because the two series must stay in step: a grant migration can only run once the object it grants on exists, so its ordering depends on the other series.
* Bad, because there are two version tables to read, two version bumps for a typical change and a second loop in the entrypoint.
* Neutral, because it still needs the same end-result test as the chosen option.

### Keep only schema changes in migrations and leave grants to database administrators

Migrations stop granting.
Operators apply grants themselves, working from `04_grants.sql` and release notes.

* Good, because migrations become simpler and grants are written only once.
* Good, because sites with their own privilege policy are not overridden.
* Bad, because correctness moves to every operator at every upgrade, which is the failure #2717 already shows: the grant had to be applied by hand on two production databases.
* Bad, because CI can no longer test that an upgraded database works, since the result depends on steps outside the repository.
* Bad, because `04_grants.sql` contains `CREATE ROLE` statements, so operators cannot simply rerun it.

### Make `04_grants.sql` replayable and run it on every start

The `CREATE USER` and `CREATE ROLE` statements move to their own `initdb.d` file or are guarded with a `create_role_if_not_exists` function, as migrations 04, 10, 13, 14 and 18 already do.
`04_grants.sql` then holds only `GRANT` statements, which are idempotent in PostgreSQL, and the entrypoint runs it after the migrations on every start.

* Good, because `04_grants.sql` becomes the single source of truth for grants, and adding a line there reaches every database on its next start.
* Good, because #2717 would have needed no migration.
* Good, because it fits the entrypoint's existing shape, which already runs the migration loop on every start.
* Bad, because it only adds: a `REVOKE`, or a grant removed from the file, still needs a migration, so the migration series keeps a role in privileges.
* Bad, because it changes the entrypoint, and a grant line that fails (for example on an object a later migration drops) stops every start of every installation until the file is fixed.
* Bad, because it reapplies the project's grants on every start, overriding any deliberate local revocation.

## More Information

The question was raised and discussed at the sda-collab meeting on 2026-11-02.

* [#2717](https://github.com/neicnordic/sensitive-data-archive/pull/2717) adds the `sda.dataset_references` grant for `download` to `04_grants.sql`.
* [#2676](https://github.com/neicnordic/sensitive-data-archive/pull/2676) adds `mapper` grants to both `04_grants.sql` and its migration script.
* [#2674](https://github.com/neicnordic/sensitive-data-archive/pull/2674) adds timing and a seeded large-database job to `.github/integration/postgres.yml`, the compose file the privilege comparison would also run in.
