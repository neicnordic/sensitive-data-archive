# NeIC SDA database definitions and docker image

We use
[Postgres 18](https://github.com/docker-library/postgres/tree/master/18/alpine)
and Alpine 3.23. Images `v3.1.63` and earlier were built on Postgres 15; see
[Upgrading from the PostgreSQL 15 image](#upgrading-from-the-postgresql-15-image)
before moving an existing database to a newer image.

Security is hardened:

- We do not use 'trust' even for local connections
- Requiring password authentication for all
- Enforcing TLS communication
- Enforcing client-certificate verification

## Configuration

The following environment variables can be used to configure the database:

| Variable               | Description                         | Default value            |
| :--------------------- | :---------------------------------- | :----------------------- |
| PGDATA                 | Mountpoint for the writable volume  | /var/lib/postgresql/data |
| POSTGRES_DB            | Name of the database                | sda                      |
| POSTGRES_PASSWORD      | Password for the user `postgres`    | -                        |
| POSTGRES_SERVER_CERT   | Public Certificate in PEM format    | -                        |
| POSTGRES_SERVER_KEY    | Private Key in PEM format           | -                        |
| POSTGRES_SERVER_CACERT | Public CA Certificate in PEM format | -                        |
| POSTGRES_VERIFY_PEER   | Enforce client verification         | verify-ca                |

Client verification is enforced if `POSTGRES_VERIFY_PEER` is set to `verify-ca` or `verify-full`.

## Upgrading from the PostgreSQL 15 image

Images up to `v3.1.63` run PostgreSQL 15. From `v3.1.64` on the image runs PostgreSQL 18. A data directory written by
PostgreSQL 15 cannot be opened by 18: the container exits with
`FATAL: database files are incompatible with server` and, in Kubernetes, the pod crash loops. This also applies to
installations made with the deprecated `sda-db` chart, whose default image is still a PostgreSQL 15 build.

The image only ships the PostgreSQL 18 binaries, and `pg_upgrade` needs the old ones next to them, so use a dump and restore instead.
The steps below were verified with `v3.1.37-postgres` as the source and `v4.0.0-postgres` as the target. They keep the
schema version, the data and the grants, and let the entrypoint run the pending schema migrations on the first start.

1. Stop all SDA services that write to the database.
2. With the old image still running, dump the `sda` database. Only `postgres` owns objects in it, so `--no-owner` is
   safe. Do not add `--no-privileges`: the service roles need the grants afterwards.

   ```sh
   pg_dump -U postgres -d sda --no-owner > sda-dump.sql
   ```

3. Stop the old container and start the new image on an **empty** volume. Do not point it at the old data directory.
   The entrypoint runs `initdb`, creates the service roles and creates `sda` at the latest schema version.
4. Replace that fresh `sda` database with the dump:

   ```sh
   psql -U postgres -d postgres -c 'DROP DATABASE sda;' -c 'CREATE DATABASE sda;'
   psql -U postgres -d sda -v ON_ERROR_STOP=1 -f sda-dump.sql
   ```

   `sda.dbschema_version` now shows the version the old instance was at.

5. Restart the container. Because the data directory is initialised, the entrypoint takes the migration path and applies
   every schema migration newer than the restored version. The log shows one `Running migration script` line per applied
   script.
6. Check `SELECT max(version) FROM sda.dbschema_version;` matches the newest file in `migratedb.d`, then start the SDA
   services again.

If the gap between the two versions contains a schema version with data migration instructions (see below), run those
around step 5 as their documents describe.

## Data migration instructions docs

In [data_migration.docs](data_migration.docs) directory there are instructions on how to execute the data migration 
if upgrading a system with existing data related to specific versions of the schema.

The file naming convention is as follows: `${SCHEMA_VERSION}_${pre/post}_${SHORT_DESCRIPTION}.md`.
* `${SCHEMA_VERSION}` - describes the schema version the data migration instructions relates to. 
* `${pre/post}` describes if these instructions should be executed before or after the schema migration has taken place.
* `${SHORT_DESCRIPTION}` - short description describing the data migration

Before upgrading the schema, check whether any data migrations are required for the schema versions you plan to apply.
Apply any **pre** data migrations **before** running the corresponding schema migration, and any **post** data migrations **after** it.

Recommended sequence when deploying:

1. Check the currently applied schema version and the target schema version after deployment.
2. Check if there are any data migration instructions for any of the schema versions to be applied.
3. For each schema version to be applied:
   1. Run pre data migrations ***if present***.
   2. Apply schema migration.
   3. Run post data migrations ***if present***.

## Schema migration rollback

In [rollback.docs](rollback.docs) directory there are instructions on how to rollback schema migrations.

The file naming convention is as follows: `${SCHEMA_VERSION}_${SHORT_DESCRIPTION}.rollback.md`.
* `${SCHEMA_VERSION}` - describes the schema version the rollback instructions relates to.
* `${SHORT_DESCRIPTION}` - short description describing the schema migration - should be the same as the schema migration in [migratedb.d](migratedb.d) 