# NeIC SDA database definitions and docker image

We use
[Postgres 15](https://github.com/docker-library/postgres/tree/master/15/alpine)
and Alpine 3.23.

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

## Data migration instructions docs

In [data_migration.docs](data_migration.docs) directory there are instructions on how to execute the data migration 
if upgrading a system with existing data related to specific versions of the schema.

The file naming convention is as follows: `${SCHEMA_VERSION}_${pre/post}_${SHORT_DESCRIPTION}.md`.
* `${SCHEMA_VERSION}` - describes the schema version the data migration instructions relates to. 
* `${pre/post}` describes if these instructions should be executed before or after the schema migration has taken place.
* `${SHORT_DESCRIPTION}` - short description describing the data migration

## Schema migration rollback

In [rollback.docs](rollback.docs) directory there are instructions on how to rollback schema migrations.

The file naming convention is as follows: `${SCHEMA_VERSION}_${SHORT_DESCRIPTION}.rollback.md`.
* `${SCHEMA_VERSION}` - describes the schema version the rollback instructions relates to.
* `${SHORT_DESCRIPTION}` - short description describing the schema migration - should be the same as the schema migration in [migratedb.d](migratedb.d) 