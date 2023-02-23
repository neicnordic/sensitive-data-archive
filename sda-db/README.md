# NeIC SDA database definitions and docker image

We use
[Postgres 13](https://github.com/docker-library/postgres/blob/517c64f87e6661366b415df3f2273c76cea428b0/13/alpine)
and Alpine 3.14.

Security is hardened:

- We do not use 'trust' even for local connections
- Requiring password authentication for all
- Using scram-sha-256 is stronger than md5
- Enforcing TLS communication
- Enforcing client-certificate verification

## Configuration

There are 2 users (`lega_in` and `lega_out`), and 2 schemas
(`local_ega` and `local_ega_download`).  A special one is included for
EBI to access the data through `local_ega_ebi`.

**note, a data volume is expected to be mounted at `$PGDATA`**

The following environment variables can be used to configure the database:

|             Variable | Description                       | Default value       |
| -------------------: | :-------------------------------- | :------------------ |
|               PGDATA | Mountpoint for the writable volume | /var/lib/postgresql/data |
|  DB_LEGA_IN_PASSWORD | `lega_in`'s password              | -                   |
| DB_LEGA_OUT_PASSWORD | `lega_out`'s password             | -                   |

## TLS support

|       Variable | Description                         | Default value           |
| -------------: | :---------------------------------- | :---------------------- |
| PG_SERVER_CERT | Public Certificate in PEM format    | `/var/lib/postgresql/certs/pg.cert` |
|  PG_SERVER_KEY | Private Key in PEM format           | `/var/lib/postgresql/certs/pg.key`  |
|          PG_CA | Public CA Certificate in PEM format | `/var/lib/postgresql/certs/CA.cert` |
| PG_VERIFY_PEER | Enforce client verification         | verify-ca               |
|          NOTLS | Disable TLS for the Postgres server | -                       |

Client verification is enforced if `PG_VERIFY_PEER` is set to `verify-ca` or `verify-full`, to disable client verification set `PG_VERIFY_PEER` to `no-verify`.

If the variable `NOTLS` exists TLS will be disabled, **not recommended for production use**.
