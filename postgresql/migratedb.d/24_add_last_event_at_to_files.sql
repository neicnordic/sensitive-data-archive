
DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 23;
  changes VARCHAR := 'Add last_event_at column to files for pagination performance';
BEGIN
  IF (select max(version) from sda.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO sda.dbschema_version VALUES(sourcever+1, now(), changes);

    -- Add denormalized column
    ALTER TABLE sda.files ADD COLUMN last_event_at TIMESTAMP WITH TIME ZONE;

    -- Backfill from existing event log data
    UPDATE sda.files AS f
    SET last_event_at = sub.started_at
    FROM (
        SELECT DISTINCT ON (file_id) file_id, started_at
        FROM sda.file_event_log
        ORDER BY file_id, started_at DESC
    ) AS sub
    WHERE f.id = sub.file_id;

    -- Create index for keyset pagination (submission_user, last_event_at DESC, id DESC)
    CREATE INDEX files_user_last_event_pagination_idx
        ON sda.files(submission_user, last_event_at DESC, id DESC);

    -- Create trigger to keep last_event_at in sync on future inserts
    CREATE FUNCTION sda.update_files_last_event_at()
    RETURNS TRIGGER AS $update_files_last_event_at$
    BEGIN
        UPDATE sda.files SET last_event_at = GREATEST(last_event_at, NEW.started_at) WHERE id = NEW.file_id;
        RETURN NEW;
    END;
    $update_files_last_event_at$ LANGUAGE plpgsql;

    CREATE TRIGGER file_event_log_update_last_event
        AFTER INSERT ON sda.file_event_log
        FOR EACH ROW
        EXECUTE PROCEDURE sda.update_files_last_event_at();

  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$
