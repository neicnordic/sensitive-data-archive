#!/bin/bash
# Restore the old-schema fixture into an empty PGDATA so that the migrate
# service in postgres.yml starts on it and runs the migration scripts.
#
# With SEED_FILES set, also migrate to schema version SEED_SCHEMA_VERSION
# (default 23) and fill sda.files and sda.file_event_log with that many
# files, SEED_EVENTS_PER_FILE events each, so the migrations after that
# version run on a production-size table (postgres-scale.yml).
set -eo pipefail

if [ "$(id -u)" = '0' ]; then
	# docker creates the volume directory as root
	mkdir -p "$PGDATA"
	chown postgres "$PGDATA"
	exec su postgres -s /bin/bash "$0" "$@"
fi

if [ -s "$PGDATA/PG_VERSION" ]; then
	echo "$PGDATA is already initialised, not seeding"
	exit 0
fi

initdb --username=postgres --pwfile=<(printf "%s\n" "$POSTGRES_PASSWORD")
pg_ctl -D "$PGDATA" -o "-c listen_addresses=''" -w start

psql -v ON_ERROR_STOP=1 --username postgres --dbname postgres -c "CREATE DATABASE sda;"
psql -v ON_ERROR_STOP=1 --username postgres --dbname sda --quiet -f "$1"

if [ -n "$SEED_FILES" ]; then
	# same loop as the entrypoint, limited to the scripts up to the seed version
	while :; do
		changed=0
		for f in /migratedb.d/*.sql; do
			n=$(basename "$f")
			n=${n%%[_.]*}
			[ "$n" -le "${SEED_SCHEMA_VERSION:-23}" ] || continue
			out=$(psql -v ON_ERROR_STOP=1 --username postgres --dbname sda -f "$f" 2>&1)
			case "$out" in *'Doing migration from'*) changed=1 ;; esac
		done
		[ "$changed" -eq 1 ] || break
	done
	echo "Seed database is at schema version $(psql -At --username postgres --dbname sda -c 'select max(version) from sda.dbschema_version')"

	echo "Seeding $SEED_FILES files with ${SEED_EVENTS_PER_FILE:-5} events each"
	psql -v ON_ERROR_STOP=1 --username postgres --dbname sda --quiet \
		-v files="$SEED_FILES" -v events="${SEED_EVENTS_PER_FILE:-5}" <<-'EOSQL'
		INSERT INTO sda.files (submission_user, submission_file_path, submission_location, archive_file_path, archive_location, submission_file_size, archive_file_size, decrypted_file_size, encryption_method)
		SELECT 'user' || (g % 50), 'project/' || (g % 1000) || '/file-' || g || '.c4gh', 'inbox', '/archive/' || gen_random_uuid(), 'archive', 1000000, 1000064, 999000, 'CRYPT4GH'
		FROM generate_series(1, :files) AS g;

		-- events in time order so the last one in the list is the latest; the
		-- message column carries a broker message about the size of a real one
		INSERT INTO sda.file_event_log (file_id, event, user_id, started_at, message)
		SELECT f.id, e.event, 'ingest', now() - interval '1 day' + e.n * interval '1 minute',
		       jsonb_build_object('type', 'ingest', 'user', f.submission_user, 'filepath', f.submission_file_path,
		                          'encrypted_checksums', jsonb_build_array(jsonb_build_object('type', 'sha256', 'value', md5(f.id::text) || md5(f.submission_file_path))))
		FROM sda.files AS f
		CROSS JOIN (SELECT n, event FROM unnest(ARRAY['registered', 'uploaded', 'submitted', 'ingested', 'archived', 'verified', 'ready']) WITH ORDINALITY AS t(event, n) WHERE n <= :events) AS e;

		ANALYZE sda.files;
		ANALYZE sda.file_event_log;
	EOSQL
fi

pg_ctl -D "$PGDATA" -m fast -w stop
