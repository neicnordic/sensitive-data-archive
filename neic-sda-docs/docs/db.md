Database Setup
==============

We use a Postgres database (version 13+ ) to store intermediate data, in
order to track progress in file ingestion. The `lega` database schema is
documented below.

> NOTE:
> Source code repository for DB component is available at:
> <https://github.com/neicnordic/sda-db>

The database container will initialize and create the necessary database
structure and functions if started with an empty area. Procedures for
*backing up the database* are important but considered out of scope for
the secure data archive project.

Look at [the SQL
definitions](https://github.com/neicnordic/sda-db/tree/master/initdb.d)
if you are also interested in the database triggers.

Configuration
-------------

The following environment variables can be used to configure the
database:

Variable              | Description                        | Default value
----------------------|------------------------------------|-----------------
`PGVOLUME`            | Mountpoint for the writable volume | /var/lib/postgresql
`DB_LEGA_IN_PASSWORD` | *lega_in*'s password               | -
`DB_LEGA_OUT_PASSWORD`| *lega_out*'s password              | -
`TZ`                  | Timezone for the Postgres server   | Europe/stockholm

For TLS support use the variables below:

Variable         | Description                         | Default value
:----------------|:------------------------------------|:---------------
`PG_SERVER_CERT` | Public Certificate in PEM format    | `$PGVOLUME/pg.cert`
`PG_SERVER_KEY`  | Private Key in PEM format           | `$PGVOLUME/pg.key`
`PG_CA`          | Public CA Certificate in PEM format | `$PGVOLUME/CA.cert`
`PG_VERIFY_PEER` | Enforce client verification         | 0
`SSL_SUBJ`       | Subject for the self-signed certificate creation | `/C=SE/ST=Sweden/L=Uppsala/O=NBIS/OU=SysDevs/CN=LocalEGA`

> NOTE:
> If not already injected, the files located at `PG_SERVER_CERT` and
> `PG_SERVER_KEY` will be generated, as a self-signed public/private
> certificate pair, using `SSL_SUBJ`. Client verification is enforced if
> and only if `PG_CA` exists and `PG_VERIFY_PEER` is set to `1`.

Database schema
---------------

The current database schema is documented below.

### Database schema migration

For continuity/ease of upgrade in production the database supports
automatic migrations between schema versions. This is handled by
migration scripts that each provide the migration from a specific schema
version to the next.

A schema version can contain multiple changes, but it is recommended to
group them logically. Some practical thinking is also useful - if larger
changes are required that risk being time consuming on large databases,
it may be best to split that work in small chunks.

Doing so helps in both demonstrating progress as well as avoiding
rollbacks of the entire process (and thus working needing to be done) if
something fails. Each schema migration is done in a transaction.

Schema versions are integers. There is no strong coupling between
releases of the secure data archive and database schema versions. A new
secure data archive release may increase several schema
versions/migrations or none.

> IMPORTANT:
> Any changes done to database schema initialization should be reflected
> in a schema migration script.

Whenever you need to change the database schema, we recommended changing
both the database initialization scripts (and bumping the bootstrapped
schema version) as well as creating the corresponding migration script
to perform the changes on a database in use.

Migration scripts should be placed in `/migratedb.d/` in the sda-db repo
(<https://github.com/neicnordic/sda-db>). We recommend naming them
corresponding to the schema version they provide migration to. There is
an "empty" migration script (`01.sql`) that can be used as a
template.
