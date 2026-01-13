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

## verify that migrations worked
migratedb=$(find /migratedb.d/ -name "*.sql" -printf '%f\n' |  sort -n | tail -1 | cut -d '.' -f1 | cut -d '_' -f1)
version=$(psql -U postgres -h migrate -d sda -At -c "select max(version) from sda.dbschema_version;")
if [ "$version" -ne "$migratedb" ]; then
    echo "Migration scripts failed"
    exit 1
fi

## verify all users can connect
for u in download finalize inbox ingest mapper sync verify; do
    export PGPASSWORD="$u"
    psql -U "$u" -h postgres -d sda -At -c "SELECT version();" 1>/dev/null
done

echo "10_sanity_check completed successfully"
