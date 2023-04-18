# NeIC SDA database definitions and docker image

We use
[Postgres 15](https://github.com/docker-library/postgres/tree/master/15/alpine)
and Alpine 3.17.

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
