---
status: proposed
date: "2026-11-26"
decision-makers:
  - "@neicnordic/sensitive-data-development-collaboration"
consulted:
  - "@kjellp"
  - "@yasinmiran"
---

# Keep all grants in one replayable file, applied separately from the migrations

## Context and Problem Statement

The database schema is defined twice.
`postgresql/initdb.d/` builds a fresh database at the newest schema version, with `04_grants.sql` creating the 13 service users and roles and holding about 140 `GRANT` lines.
`postgresql/migratedb.d/` brings an existing database from an older version to the newest one.
The entrypoint of the project's PostgreSQL container only runs `initdb.d` when the data directory is empty: once `$PGDATA/PG_VERSION` exists it runs the `migratedb.d` scripts until none reports `Doing migration from`, sets the `lega_in` and `lega_out` passwords and writes `pg_hba.conf`.
A line added to `04_grants.sql` therefore never reaches a database that was initialised by an earlier release.

Not every installation runs that container, and the answer has to work for all three kinds:

1. An installation that runs the project's PostgreSQL container, where the entrypoint applies the migrations.
2. An installation whose database is run by a separate team that only receives the SQL files and applies them by hand.
3. An installation where the roles and the lifecycle of the database server are managed declaratively, and the migrations are applied by hand or a new database is built from `initdb.d` and filled through logical replication.

The second and third manage privileges themselves, so they need to apply the schema migrations without the project's grants.

Until now the two definitions have been kept in step by habit.
Almost every migration from 02 to 22 that creates a table or a role also grants on it in the same file (22, for example, creates `sda.file_headers_backup` and grants `INSERT, SELECT, UPDATE` on it to `rotatekey`).
Migration 12 is the exception: it creates `sda.encryption_keys` without a grant, and the grants on it arrive later, in 13, 16 and 18.
Migrations 23 to 28 add columns, indexes, a row and functions, which need no new grant: columns and indexes are covered by the table's grants, and new functions are executable by `PUBLIC` by default.
Migration 29 grants `DELETE ON sda.file_dataset` and `SELECT ON sda.dataset_event_log` to `mapper`, both on tables that already existed.
The habit was never written down, and it has slipped three times:

* `GRANT INSERT ON sda.encryption_keys TO ingest` is in `04_grants.sql` and in no migration, so a database upgraded from an older release lacks it today.
* #2717 added `GRANT SELECT ON sda.dataset_references TO download;` to `04_grants.sql` only.
  The table exists on every database (it is created in `01_main.sql` and in migration 04), so an upgraded database has the table and lacks the grant.
  Two production databases received it by hand.
  #2717 was closed without merging, and #2748 replaces it with the same grant in a migration script of its own, `30_grant_dataset_references_to_download.sql`.
* #2676 added `mapper` grants to `04_grants.sql`.
  Its first version had no matching grants in its migration script; the version that was merged has them, in migration 29.

The failure shows up after an upgrade as a permission error in a service such as `sda-doa` or `mapper`, which reads like a bug in the service.
The sharpest case is a new table created in a migration whose grant lives only in `04_grants.sql`: a fresh database works, an upgraded one does not.

CI already builds both kinds of database side by side.
`.github/integration/postgres.yml` starts a `postgres` service from an empty volume and a `migrate` service on a volume that the `migrate_seed` service first fills with the schema version 0 fixture (through `seed_migrate_db.sh`), so the entrypoint migrates it to the newest version.
`10_sanity_check.sh` checks that `migrate` started from the fixture and reached the newest version.
The per-role query scripts (`20_inbox_queries.sh` to `70_download_queries.sh`) run against both services, but only cover the queries someone has written a test for.
Nothing compares the privileges of the two databases.

How should the project keep the grants of a migrated database identical to those of a fresh one, for every kind of installation?

## Decision Drivers

* A database upgraded from any earlier release ends up with the same privileges as a fresh one.
* A missing grant fails in CI, not in a service after deployment.
* It works for all three kinds of installation: one that manages privileges itself can apply the schema migrations without the project's grants.
* A privilege that is taken away is as hard to forget as one that is added.
* Little new machinery: the migration series, the entrypoint and the CI compose file already exist.

## Considered Options

* Keep grants in the schema migration that creates the need, and check the end result.
* Maintain grants in a separate migration series next to the schema migrations.
* Keep only schema changes in migrations and leave grants to database administrators.
* Make `04_grants.sql` replayable and run it on every start.
* Keep all grants in one replayable file, applied separately from the migrations.

## Decision Outcome

Chosen option: "Keep all grants in one replayable file, applied separately from the migrations", because it lets an installation apply the schema migrations without the project's grants and still writes each privilege once.
Removing a line from the file removes the privilege, which no other option gives.

* A new migration changes the schema only and contains no `GRANT` or `REVOKE`.
  The migrations that are already merged keep the grants they contain.
* `04_grants.sql` holds every privilege the services need and nothing else, in a single transaction: it first revokes all privileges on the project's schemas, tables and sequences from the project's roles, then grants what is listed.
* The `CREATE USER` and `CREATE ROLE` statements move to their own `initdb.d` file.
  They only run when the current user is `postgres`, and an installation that creates its roles elsewhere leaves the file out.
* An upgrade is two steps: the new migrations, then the grants file.
* In the project's container the entrypoint applies the file after the migration loop.
  A failure is logged and does not stop the start, and the step can be switched off.
* An installation that only receives the SQL files applies the file by hand, or compares it between two releases to see what changed.
* An installation that manages privileges declaratively does not run it and reads it as the list of what each service needs.
* A grant on an existing object, such as the one in #2748, becomes a line in `04_grants.sql` and needs no migration script.

### Consequences

* Good, because privileges are written once, and a pull request that changes them touches one file.
* Good, because the same file serves all three kinds of installation: one runs it, one applies or compares it by hand, one reads it.
* Good, because a grant that was changed by hand on a live database is put back at the next apply.
* Bad, because an upgrade outside the project's container has a second step, and forgetting it leaves a service without a privilege.
* Bad, because the revoke also removes anything an installation granted to the project's roles on its own.
* Bad, because the entrypoint changes.
* Bad, because the migrations that are already merged still contain grants, so the split is clean only for new ones.

### Confirmation

* A new script in `.github/integration/tests/postgres/` dumps, on both the `migrate` and the `postgres` service, the privileges on every table, view, sequence and function in the `sda`, `local_ega`, `local_ega_download` and `local_ega_ebi` schemas and on the schemas themselves (from `pg_class.relacl`, `pg_proc.proacl` and `pg_namespace.nspacl`, expanded with `aclexplode`), together with role memberships from `pg_auth_members`.
  It sorts both lists and fails on any difference, printing the lines that differ.
  Role attributes are left out, because the `credentials` service sets `LOGIN` on both databases.
* The two databases differ today.
  The schema version 0 fixture grants privileges on the legacy `local_ega` tables directly to `lega_in` and `lega_out`, and migrations 02 and 03 add bulk `GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA` statements, while a fresh database gives the same users their rights through role membership.
  A grants file that revokes first should remove those differences, and the comparison shows whether it does.
  Any difference that remains is listed in the test as a commented exception, so that every known deviation is written down and reviewed.
* The same job applies the grants file a second time and checks that the privileges do not change.
* Before the entrypoint change is merged, the grants file is applied in the CI compose stack while the services are running, to see what the transaction does to them.
  This has not been tested yet.
* `postgresql/README.md` gets a short section stating the rule above and the two upgrade steps.
* A CI step fails a pull request that adds a `GRANT` or `REVOKE` to a new file in `postgresql/migratedb.d/`.

## Pros and Cons of the Options

### Keep grants in the schema migration that creates the need, and check the end result

A migration that creates a table, sequence, function or role also grants on it in the same file.
A change to the privileges on an existing object gets its own migration script with a version bump, and `04_grants.sql` stays the fresh-database picture of the same end state.
Migration 29 and #2748 already follow this.

* Good, because it is what most of the migrations from 02 to 22 already do, so no existing script changes.
* Good, because one migration file describes one complete step: the object and who may use it.
* Good, because the entrypoint stays as it is.
* Bad, because the grant is duplicated between the migration and `04_grants.sql`.
* Bad, because without the CI check it relies on the same habit that has already slipped.
* Bad, because a grant-only change costs a schema version bump.
* Bad, because an installation that manages privileges itself cannot apply a migration without its grants, and has to pick them out of each script.

### Maintain grants in a separate migration series next to the schema migrations

Schema changes stay in `migratedb.d/`, and grant changes go to a second numbered series with its own version table, run after the schema migrations.

* Good, because privileges have a clear home, and a grant-only change does not bump the schema version.
* Good, because an installation that manages privileges itself can skip the second series.
* Bad, because the two series must stay in step: a grant migration can only run once the object it grants on exists, so its ordering depends on the other series.
* Bad, because there are two version tables to read, two version bumps for a typical change and a second loop in the entrypoint.
* Neutral, because it still needs the same end-result test as the other options.

### Keep only schema changes in migrations and leave grants to database administrators

Migrations stop granting.
Operators apply grants themselves, working from `04_grants.sql` and release notes.

* Good, because migrations become simpler and grants are written only once.
* Good, because sites with their own privilege policy are not overridden.
* Bad, because correctness moves to every operator at every upgrade, which is the failure #2717 showed: the grant had to be applied by hand on two production databases.
* Bad, because CI can no longer test that an upgraded database works, since the result depends on steps outside the repository.
* Bad, because `04_grants.sql` contains `CREATE ROLE` statements, so operators cannot simply rerun it.

### Make `04_grants.sql` replayable and run it on every start

The `CREATE USER` and `CREATE ROLE` statements move to their own `initdb.d` file or are guarded with a `create_role_if_not_exists` function, as migrations 04, 10, 13, 14 and 18 already do.
`04_grants.sql` then holds only `GRANT` statements, which are idempotent in PostgreSQL, and the entrypoint runs it after the migrations on every start.

* Good, because `04_grants.sql` becomes the single source of truth for grants, and adding a line there reaches every database on its next start.
* Good, because #2717 would have needed no migration.
* Good, because it fits the entrypoint's existing shape, which already runs the migration loop on every start.
* Bad, because it only adds: a `REVOKE`, or a grant removed from the file, still needs a migration, so the migration series keeps a role in privileges.
* Bad, because running it on every start ties it to the project's container, which only one of the three kinds of installation runs.
* Bad, because it changes the entrypoint, and a grant line that fails (for example on an object a later migration drops) stops every start of every installation until the file is fixed.
* Bad, because it reapplies the project's grants on every start, overriding any deliberate local revocation.

### Keep all grants in one replayable file, applied separately from the migrations

New migrations change the schema only and contain no `GRANT` or `REVOKE`; the existing ones stay as they are.
The `CREATE USER` and `CREATE ROLE` statements move to their own `initdb.d` file, which only runs as `postgres` and which an installation that creates its roles elsewhere can leave out.
`04_grants.sql` then holds every privilege the services need and nothing else, in a single transaction: it first revokes all privileges on the project's schemas, tables and sequences from the project's roles, then grants what is listed.
An upgrade becomes two steps, the new migrations and then the grants file.
The entrypoint applies the file after the migration loop unless it is told to skip it, and a failure there is logged and does not stop the start.
An installation that only receives the SQL files applies it by hand, or compares the file between two releases to see what changed.
An installation that manages privileges declaratively does not run it and reads it as the list of what each service needs.

* Good, because privileges are written once, and a line removed from the file is a privilege removed at the next apply, with no `REVOKE` to remember.
* Good, because an installation that applies the migrations but manages privileges itself no longer has to pick grants out of migration scripts.
* Good, because the file is plain SQL that does not depend on the purpose-built container or its entrypoint.
* Good, because a statement that fails rolls the whole file back and leaves the privileges as they were.
* Neutral, because it still needs the same end-result test as the other options, here comparing a fresh database with one that had the migrations and then the grants file applied.
* Neutral, because functions stay executable by `PUBLIC` by default, so the file does not govern them unless that default is revoked as well.
* Bad, because it changes the entrypoint and makes an upgrade two steps: an operator who applies the migrations and forgets the grants file ends up with a service that lacks a privilege.
* Bad, because the revoke removes anything an installation granted to the project's roles on its own, and the first apply on an existing database removes hand-applied grants that are not in the file.
* Bad, because whoever applies the file has to own the objects or be a superuser; otherwise privileges granted by another role stay in place.
* Bad, because the migrations that are already merged keep the grants they contain, so an installation that upgrades from an old release without them still has to strip those by hand.
* Bad, because it does not yet say how a role added in a later release reaches an upgraded database: the file that creates roles is in `initdb.d`, which an existing database never runs, and the grants file fails on a role that is missing.
* Bad, because the effect of the revoke and grant transaction on services that are running has not been tested.

## More Information

The question was raised at the sda-collab meeting on 2026-11-02.
The first version of this record proposed keeping grants in the schema migrations.
The reviews on the pull request and the meeting on 2026-11-16 brought up the installations that do not run the project's container, and the fifth option was added in answer.

* [#2717](https://github.com/neicnordic/sensitive-data-archive/pull/2717) added the `sda.dataset_references` grant for `download` to `04_grants.sql` only, and was closed without merging.
* [#2748](https://github.com/neicnordic/sensitive-data-archive/pull/2748) replaces it, with the grant in `04_grants.sql` and in a migration script.
* [#2676](https://github.com/neicnordic/sensitive-data-archive/pull/2676), merged, adds `mapper` grants to both `04_grants.sql` and migration 29.
* [#2674](https://github.com/neicnordic/sensitive-data-archive/pull/2674) adds timing and a seeded large-database job to `.github/integration/postgres.yml`, the compose file the privilege comparison would also run in.
