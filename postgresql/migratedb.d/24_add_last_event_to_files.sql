
DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 23;
  changes VARCHAR := 'Add last_event column to files to avoid join on file_event_log';
  -- The backfill walks sda.files in physical order, one range of heap blocks
  -- per transaction, and commits as it goes. 5000 blocks is about 40 MB,
  -- small enough that a batch commits well inside the minute a liveness
  -- probe allows before it restarts the pod.
  blocks_per_batch CONSTANT INTEGER := 5000;
  b BIGINT := 0;
  nblocks BIGINT;
  updated BIGINT;
  total BIGINT := 0;
BEGIN
  IF (select max(version) from sda.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;

    -- This migration commits in steps instead of running as one transaction,
    -- because the backfill rewrites every row in sda.files. Each step is safe
    -- to repeat, and the schema version is recorded last, so an interrupted
    -- run continues where it stopped the next time the migrations run.
    -- Sites with a large files table can run the backfill ahead of the
    -- upgrade, see data_migration.docs/24_pre_backfill_last_event.md.

    -- Add denormalized column storing the latest event name (e.g. 'registered', 'uploaded')
    ALTER TABLE sda.files ADD COLUMN IF NOT EXISTS last_event TEXT REFERENCES sda.file_events(title);

    -- Disable the files_last_modified trigger so the backfill does not
    -- overwrite last_modified / last_modified_by on every existing row.
    ALTER TABLE sda.files DISABLE TRIGGER files_last_modified;
    COMMIT;

    -- Latest event per file, computed once with a single pass over the event log.
    CREATE TEMPORARY TABLE last_event_backfill AS
    SELECT DISTINCT ON (file_id) file_id, event
    FROM sda.file_event_log
    WHERE file_id IS NOT NULL
    ORDER BY file_id, started_at DESC;
    ALTER TABLE last_event_backfill ADD PRIMARY KEY (file_id);
    ANALYZE last_event_backfill;
    COMMIT;

    -- Backfill in block ranges. The ids are random UUIDs, so a batch keyed on
    -- the primary key would touch one random heap page per row; a TID range
    -- reads the table sequentially. Rows updated earlier get new versions that
    -- may land past the current end of the table, so the size is re-read each
    -- round; those rows already have last_event set and are skipped.
    LOOP
      nblocks := pg_relation_size('sda.files') / current_setting('block_size')::INTEGER;
      EXIT WHEN b > nblocks;

      UPDATE sda.files AS f
      SET last_event = t.event
      FROM last_event_backfill AS t
      WHERE t.file_id = f.id
        AND f.ctid >= format('(%s,0)', b)::tid
        AND f.ctid <  format('(%s,0)', b + blocks_per_batch)::tid
        AND f.last_event IS NULL
        AND t.event IS NOT NULL;   -- a NULL event would leave the row eligible for every later batch

      GET DIAGNOSTICS updated = ROW_COUNT;
      total := total + updated;
      b := b + blocks_per_batch;
      COMMIT;
      RAISE NOTICE 'last_event backfill: block % of %, % rows in this batch, % rows in total', b, nblocks, updated, total;
    END LOOP;

    DROP TABLE last_event_backfill;
    ALTER TABLE sda.files ENABLE TRIGGER files_last_modified;

    -- Create trigger to keep last_event in sync on future inserts
    CREATE OR REPLACE FUNCTION sda.update_files_last_event()
    RETURNS TRIGGER
    LANGUAGE plpgsql
    SECURITY DEFINER
    SET search_path = pg_catalog, sda
    AS $update_files_last_event$
    BEGIN
        UPDATE sda.files
        SET last_event = NEW.event
        WHERE id = NEW.file_id
          AND NOT EXISTS (
              SELECT 1
              FROM sda.file_event_log AS fel
              WHERE fel.file_id = NEW.file_id
                AND fel.started_at > NEW.started_at
          );
        RETURN NEW;
    END;
    $update_files_last_event$;

    DROP TRIGGER IF EXISTS file_event_log_update_last_event ON sda.file_event_log;
    CREATE TRIGGER file_event_log_update_last_event
        AFTER INSERT ON sda.file_event_log
        FOR EACH ROW
        EXECUTE PROCEDURE sda.update_files_last_event();

    INSERT INTO sda.dbschema_version VALUES(sourcever+1, now(), changes);

  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$
