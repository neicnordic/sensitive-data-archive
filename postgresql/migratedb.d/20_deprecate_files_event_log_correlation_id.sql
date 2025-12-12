
DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 19;
  changes VARCHAR := 'Deprecate file_event_log.correlation_id column and migrate data where file_id != correlation_id';
BEGIN
  IF (select max(version) from sda.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO sda.dbschema_version VALUES(sourcever+1, now(), changes);

    -- Check that we have no file_event_log WHERE file_id != correlation_id before proceeding with migration
    IF (SELECT COUNT(*) FROM sda.file_event_log WHERE file_id != correlation_id AND correlation_id IS NOT NULL) > 0 then
        RAISE EXCEPTION 'Cannot proceed with migration since file_event_log rows exists where file_id != correlation_id';
    END IF;

    -- Update RegisterFile func
    -- First drop it so we can create the updated version
    DROP FUNCTION IF EXISTS sda.register_file;

    -- Create updated function
    -- Function for registering files on upload
    CREATE FUNCTION sda.register_file(file_id TEXT, submission_file_path TEXT, submission_user TEXT)
        RETURNS TEXT AS $register_file$
    DECLARE
    file_uuid UUID;
    BEGIN
        -- Upsert file information. we're not interested in restarted uploads so old
        -- overwritten files that haven't been ingested are updated instead of
        -- inserting a new row.
    INSERT INTO sda.files( id, submission_file_path, submission_user, encryption_method )
    VALUES(  COALESCE(CAST(NULLIF(file_id, '') AS UUID), gen_random_uuid()), submission_file_path, submission_user, 'CRYPT4GH' )
        ON CONFLICT ON CONSTRAINT unique_ingested
        DO UPDATE SET submission_file_path = EXCLUDED.submission_file_path,
               submission_user = EXCLUDED.submission_user,
               encryption_method = EXCLUDED.encryption_method
               RETURNING id INTO file_uuid;

    -- We add a new event for every registration though, as this might help for
    -- debugging.
    INSERT INTO sda.file_event_log( file_id, event, user_id )
    VALUES (file_uuid, 'registered', submission_user);

    RETURN file_uuid;
    END;
    $register_file$ LANGUAGE plpgsql;

    -- Drop the correlation_id column from sda.file_event_log
    ALTER TABLE sda.file_event_log
        DROP COLUMN correlation_id;

ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$
