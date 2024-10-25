#!/bin/sh
set -e

apt-get -o DPkg::Lock::Timeout=60 update > /dev/null
apt-get -o DPkg::Lock::Timeout=60 install -y postgresql-client >/dev/null

for n in api auth download finalize inbox ingest mapper sync verify; do
    echo "creating credentials for: $n"
    psql -U postgres -h migrate -d sda -c "ALTER ROLE $n LOGIN PASSWORD '$n';"
    psql -U postgres -h postgres -d sda -c "ALTER ROLE $n LOGIN PASSWORD '$n';"
done
