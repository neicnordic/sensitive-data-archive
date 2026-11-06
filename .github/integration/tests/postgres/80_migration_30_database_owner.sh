#!/bin/bash
set -euo pipefail

database=migration30_test
runner=migration30_runner
runner_password=migration30_runner
postgres_host=${POSTGRES_HOST:-postgres}
admin_password=${PGPASSWORD:?PGPASSWORD must be set by the integration environment}

admin_psql() {
    PGPASSWORD="$admin_password" psql -v ON_ERROR_STOP=1 \
        -U postgres -h "$postgres_host" "$@"
}

cleanup_resources() {
    local status=0

    admin_psql -d postgres \
        -c "DROP DATABASE IF EXISTS $database WITH (FORCE);" >/dev/null || status=1
    admin_psql -d postgres \
        -c "DROP ROLE IF EXISTS $runner;" >/dev/null || status=1

    return "$status"
}

cleanup() {
    result=$?
    trap - EXIT
    cleanup_resources || result=1
    exit "$result"
}

cleanup_resources
trap cleanup EXIT

admin_psql -d postgres <<SQL
CREATE ROLE $runner LOGIN PASSWORD '$runner_password' NOSUPERUSER NOCREATEDB NOCREATEROLE;
CREATE DATABASE $database OWNER $runner;
SQL

PGPASSWORD="$runner_password" psql -v ON_ERROR_STOP=1 \
    -U "$runner" \
    -h "$postgres_host" \
    -d "$database" <<'SQL'
CREATE SCHEMA sda;

CREATE TABLE sda.dbschema_version (
    version INTEGER PRIMARY KEY,
    applied TIMESTAMP WITH TIME ZONE,
    description VARCHAR(1024)
);

INSERT INTO sda.dbschema_version
VALUES (29, now(), 'test fixture');

CREATE TABLE sda.dataset_references (
    id INTEGER
);
SQL

export PGPASSWORD="$runner_password"
owner_membership=$(psql -v ON_ERROR_STOP=1 -U "$runner" -h "$postgres_host" \
    -d "$database" -At \
    -c "SELECT NOT rolsuper AND pg_has_role(current_user, 'pg_database_owner', 'MEMBER') FROM pg_roles WHERE rolname = current_user;")
if [ "$owner_membership" != "t" ]; then
    echo "$runner is not a non-superuser member of pg_database_owner"
    exit 1
fi

psql -v ON_ERROR_STOP=1 -U "$runner" -h "$postgres_host" -d "$database" \
    -f /migratedb.d/30_grant_dataset_references_to_download.sql

version=$(psql -v ON_ERROR_STOP=1 -U "$runner" -h "$postgres_host" \
    -d "$database" -At \
    -c "SELECT max(version) FROM sda.dbschema_version;")
if [ "$version" != "30" ]; then
    echo "Expected schema version 30, got $version"
    exit 1
fi

has_download_access=$(psql -v ON_ERROR_STOP=1 -U "$runner" -h "$postgres_host" \
    -d "$database" -At \
    -c "SELECT has_table_privilege('download', 'sda.dataset_references', 'SELECT');")
if [ "$has_download_access" != "t" ]; then
    echo "download was not granted SELECT on sda.dataset_references"
    exit 1
fi

echo "80_migration_30_database_owner completed successfully"
