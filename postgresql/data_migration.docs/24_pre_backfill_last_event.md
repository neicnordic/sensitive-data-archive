# Data Migration Plan PRE schema migration version 24

Schema version 24 fills `sda.files.last_event` from `sda.file_event_log` for every existing file. On a small database
the container does this itself at startup. On a large one, run the migration script by hand before deploying; the
schema is then already at 24 and the container skips it.

Do this when `sda.files` has more than a few million rows, when the database runs under Kubernetes, or when the
storage is slow. The container runs migrations before the server accepts TCP connections and the `sda-db` chart's
liveness probe connects over TCP, so a migration longer than about a minute gets the pod killed. The script resumes
where it stopped, but on a large table its first step, one pass over the event log, takes longer than a minute on its
own, so it never gets to the batches. With 4.7 million files and 25 million events on storage that delivers about 40
random reads per second the backfill took 6.5 hours; on SSD it is minutes.

## 1. Check the schema version

```sql
SELECT max(version) AS current_version FROM sda.dbschema_version;
```

If the result is not 23, do not proceed.

## 2. Stop the services

Everything that writes to `sda.files` or `sda.file_event_log` must be stopped: ingest, verify, finalize, mapper, api,
s3inbox, sync, rotatekey and the validator. The database itself stays up.

## 3. Run the migration script from inside the database container

Copy [24_add_last_event_to_files.sql](../migratedb.d/24_add_last_event_to_files.sql) from this version of the
repository into the container (the running image has the old one), then run it over the Unix socket in a session that
survives losing the terminal. A client outside the cluster, through a port-forward or a load balancer, drops the
connection during a long statement.

```sh
nohup psql -v ON_ERROR_STOP=1 -U postgres -d sda \
  -c "SET work_mem = '1GB'" -c "SET synchronous_commit = off" \
  -c "ALTER TABLE sda.files SET (autovacuum_enabled = false)" \
  -f 24_add_last_event_to_files.sql \
  -c "ALTER TABLE sda.files RESET (autovacuum_enabled)" \
  -c "VACUUM ANALYZE sda.files" > backfill.log 2>&1 &
tail -f backfill.log
```

The settings keep autovacuum off the table while it is rewritten and skip the per-batch WAL flush; the vacuum at the
end removes the dead row versions the backfill leaves. Do not add `-1` or `--single-transaction`, the script commits
per batch. If the run dies, start the same command again; it continues where it stopped. If you abandon it, run the
last two `-c` statements by hand and `ALTER TABLE sda.files ENABLE TRIGGER files_last_modified`, which the script only
does at the end.

## 4. Verify

```sql
SELECT max(version) FROM sda.dbschema_version;   -- 24

-- 0: files that have events but no last_event
SELECT count(*) FROM sda.files AS f
WHERE f.last_event IS NULL
  AND EXISTS (SELECT 1 FROM sda.file_event_log AS e WHERE e.file_id = f.id);

-- both enabled (tgenabled = O)
SELECT tgname, tgenabled FROM pg_trigger
WHERE tgrelid IN ('sda.files'::regclass, 'sda.file_event_log'::regclass) AND NOT tgisinternal;
```

A file with no row at all in `sda.file_event_log` keeps `last_event = NULL`; the trigger fills it in at its next event.

## 5. Deploy

Deploy the new version. The container sees schema version 24, skips this migration and applies the later ones.
