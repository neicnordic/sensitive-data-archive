#!/bin/sh
set -eou pipefail

## The migrations ran on a seeded database while the container was killed
## every minute. Check that they got to the end anyway, and show how many
## attempts each script needed.
log=/pgmigrate/migration_timings.log

echo "script runs seconds"
awk '{ runs[$2]++; secs[$2]+=$3 } END { for (s in runs) print s, runs[s], secs[s] }' "$log" | sort

migratedb=$(find /migratedb.d/ -name "*.sql" -printf '%f\n' | sort -n | tail -1 | cut -d '.' -f1 | cut -d '_' -f1)
version=$(psql -U postgres -h migrate -d sda -At -c "select max(version) from sda.dbschema_version;")
if [ "$version" != "$migratedb" ]; then
    echo "Schema is at version $version, migrations did not reach $migratedb"
    exit 1
fi

## known case, schema version 24: every seeded file has events, so every one must have last_event
missing=$(psql -U postgres -h migrate -d sda -At -c "select count(*) from sda.files where last_event is null;")
if [ "$missing" != 0 ]; then
    echo "$missing files have no last_event after migration 24"
    exit 1
fi
