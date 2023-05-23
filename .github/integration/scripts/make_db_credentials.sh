#!/bin/sh
set -e

apt-get -o DPkg::Lock::Timeout=60 update > /dev/null
apt-get -o DPkg::Lock::Timeout=60 install -y postgresql-client >/dev/null

for n in download finalize inbox ingest mapper sync verify; do
    echo "creating credentials for: $n"

    if [ "$n" = inbox ]; then
        psql -U postgres -h postgres -d sda -c "DROP ROLE IF EXISTS inbox;"
        psql -U postgres -h postgres -d sda -c "CREATE ROLE inbox;"
        psql -U postgres -h postgres -d sda -c "GRANT base, ingest TO inbox;"
    fi

    if [ "$n" = ingest ]; then
        psql -U postgres -h postgres -d sda -c "GRANT UPDATE ON local_ega.main TO ingest;"
    fi

    psql -U postgres -h postgres -d sda -c "ALTER ROLE $n LOGIN PASSWORD '$n';"
done
