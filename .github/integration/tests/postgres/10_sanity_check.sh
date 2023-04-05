#!/bin/sh
set -eou pipefail

export PGSSLCERT=/certs/client.crt
export PGSSLKEY=/certs/client.key
export PGSSLROOTCERT=/certs/ca.crt
export PGSSLMODE=verify-ca

psql -U postgres -h postgres_tls -d sda -At -c "SELECT version();" 1>/dev/null

## verify tls settings does not allow non tls connections
unset PGSSLCERT
unset PGSSLKEY
unset PGSSLROOTCERT
unset PGSSLMODE
set +e

psql -U postgres -h postgres_tls -d sda -At -c "SELECT version();" 2>/dev/null
status=$?
if [ "$status" -eq 0 ]; then
    exit 1
fi

## verify all users can connect
for u in download finalize inbox ingest mapper sync verify; do
    export PGPASSWORD="$u"
    psql -U "$u" -h postgres -d sda -At -c "SELECT version();" 1>/dev/null
done

