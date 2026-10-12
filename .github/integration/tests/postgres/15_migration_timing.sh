#!/bin/sh
set -eou pipefail

## migrate's entrypoint appends one line per migration script run to PGDATA:
## "<epoch> <script> <seconds> <ok|failed>". A script that takes longer than
## MIGRATION_TIME_LIMIT seconds would get the pod killed by the sda-db chart's
## liveness probe, which only has a minute's patience.
log=/pgmigrate/migration_timings.log
limit="${MIGRATION_TIME_LIMIT:-60}"

if [ ! -s "$log" ]; then
    echo "No migration timings found in $log"
    exit 1
fi

echo "script runs seconds"
awk '{ runs[$2]++; secs[$2]+=$3 } END { for (s in runs) print s, runs[s], secs[s] }' "$log" | sort

slow=$(awk -v limit="$limit" '$3 > limit { print $2 " took " $3 " s" }' "$log")
if [ -n "$slow" ]; then
    echo "Migration scripts over the ${limit} s liveness budget:"
    echo "$slow"
    exit 1
fi
