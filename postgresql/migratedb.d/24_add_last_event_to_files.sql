
DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 23;
  changes VARCHAR := 'Add last_event column to files to avoid join on file_event_log';
BEGIN
  IF (select max(version) from sda.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO sda.dbschema_version VALUES(sourcever+1, now(), changes);

    -- Add denormalized column storing the latest event name (e.g. 'registered', 'uploaded')
    ALTER TABLE sda.files ADD COLUMN last_event TEXT REFERENCES sda.file_events(title);

    -- Backfill from existing event log data
    UPDATE sda.files AS f
    SET last_event = sub.event
    FROM (
        SELECT DISTINCT ON (file_id) file_id, event
        FROM sda.file_event_log
        ORDER BY file_id, started_at DESC
    ) AS sub
    WHERE f.id = sub.file_id;

    -- Create trigger to keep last_event in sync on future inserts
    CREATE FUNCTION sda.update_files_last_event()
    RETURNS TRIGGER
    LANGUAGE plpgsql
    SECURITY DEFINER
    SET search_path = pg_catalog, sda
    AS $update_files_last_event$
    BEGIN
        UPDATE sda.files SET last_event = NEW.event WHERE id = NEW.file_id;
        RETURN NEW;
    END;
    $update_files_last_event$;

    CREATE TRIGGER file_event_log_update_last_event
        AFTER INSERT ON sda.file_event_log
        FOR EACH ROW
        EXECUTE PROCEDURE sda.update_files_last_event();

  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$
