#!/bin/bash
echo "Starting database"

# initialize database
initdb --username=postgres

pg_ctl -o "-c listen_addresses='127.0.0.1'" -w start

psql -v ON_ERROR_STOP=1 --username postgres --no-password --dbname postgres <<-'EOSQL'
SET TIME ZONE 'UTC';
CREATE DATABASE lega;
EOSQL

echo
echo "Initializing schemas"
for f in /docker-entrypoint-initdb.d/*
do
	file_errors=$(psql -v ON_ERROR_STOP=1 --username postgres --no-password --dbname lega -f "$f" 2>&1 >/dev/null )
    if [[ "$file_errors" != "" ]]
    then
        errors="$errors\n$file_errors"
    fi
done

for i in {1..20}
do
    out="$(pg_isready -U postgres -h localhost)"
    if [[ "$out" != "localhost:5432 - no response" ]]
    then
        echo
        break
    fi
    printf "."
    sleep 5
done

if [[ "$out" != "localhost:5432 - no response" ]]
then
    if [[ "$errors" == "" ]]
    then
        echo "Database started successfully"
    else
        echo "Database started with errors:"
        echo -e "$errors"
    fi
else
    echo "Database failed to start"
    exit 1
fi

echo

go test ./...
