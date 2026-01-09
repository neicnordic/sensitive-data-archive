
DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 22;
  changes VARCHAR := 'Expand files table with storage locations';
BEGIN
  IF (select max(version) from sda.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO sda.dbschema_version VALUES(sourcever+1, now(), changes);

    ALTER TABLE sda.files
        ADD COLUMN archive_location TEXT,
        ADD COLUMN backup_location TEXT,
        ADD COLUMN submission_location TEXT;

    CREATE INDEX files_submission_location_idx ON sda.files(submission_location);
    CREATE INDEX files_archive_location_idx ON sda.files(archive_location);
    CREATE INDEX files_backup_location_idx ON sda.files(backup_location);

    DROP FUNCTION IF EXISTS sda.register_file;
    CREATE FUNCTION sda.register_file(file_id TEXT, submission_location TEXT, submission_file_path TEXT, submission_user TEXT)
        RETURNS TEXT AS $register_file$
    DECLARE
    file_uuid UUID;
    BEGIN
        -- Upsert file information. we're not interested in restarted uploads so old
        -- overwritten files that haven't been ingested are updated instead of
        -- inserting a new row.
    INSERT INTO sda.files( id, submission_location, submission_file_path, submission_user, encryption_method )
    VALUES(  COALESCE(CAST(NULLIF(file_id, '') AS UUID), gen_random_uuid()), submission_location, submission_file_path, submission_user, 'CRYPT4GH' )
        ON CONFLICT ON CONSTRAINT unique_ingested
        DO UPDATE SET submission_location = EXCLUDED.submission_location,
               submission_file_path = EXCLUDED.submission_file_path,
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

  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$
