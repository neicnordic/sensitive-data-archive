
DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 18;
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

    DROP FUNCTION IF EXISTS sda.register_file;
    CREATE FUNCTION sda.register_file(submission_location TEXT, submission_file_path TEXT, submission_user TEXT, corr_id TEXT)
    RETURNS TEXT AS $register_file$
    DECLARE
        file_ext TEXT;
        file_uuid UUID;
    BEGIN
        INSERT INTO sda.files( submission_location, submission_file_path, submission_user, encryption_method )
        VALUES( submission_location, submission_file_path, submission_user, 'CRYPT4GH' )
            ON CONFLICT ON CONSTRAINT unique_ingested
            DO UPDATE SET
              submission_location = EXCLUDED.submission_location,
              submission_file_path = EXCLUDED.submission_file_path,
              submission_user = EXCLUDED.submission_user,
              encryption_method = EXCLUDED.encryption_method
            RETURNING id INTO file_uuid;

        INSERT INTO sda.file_event_log( file_id, event, user_id, correlation_id)
        VALUES (file_uuid, 'registered', submission_user, COALESCE(CAST(NULLIF(corr_id, '') AS UUID), file_uuid));

        RETURN file_uuid;
    END;
    $register_file$ LANGUAGE plpgsql;

    DROP FUNCTION IF EXISTS sda.set_archived;
    CREATE FUNCTION sda.set_archived(file_uuid UUID, corr_id UUID, archive_loc TEXT, file_path TEXT, file_size BIGINT, inbox_checksum_value TEXT, inbox_checksum_type TEXT)
    RETURNS void AS $set_archived$
    BEGIN
        UPDATE sda.files SET archive_location = archive_loc, archive_file_path = file_path, archive_file_size = file_size WHERE id = file_uuid;

        INSERT INTO sda.checksums(file_id, checksum, type, source)
        VALUES(file_uuid, inbox_checksum_value, upper(inbox_checksum_type)::sda.checksum_algorithm, upper('UPLOADED')::sda.checksum_source);

        INSERT INTO sda.file_event_log(file_id, event, correlation_id) VALUES(file_uuid, 'archived', corr_id);
    END;
    $set_archived$ LANGUAGE plpgsql;

  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$
