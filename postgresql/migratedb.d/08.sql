DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 7;
  changes VARCHAR := 'Add ingestion functions ';
BEGIN
-- No explicit transaction handling here, this all happens in a transaction
-- automatically
  IF (select max(version) from sda.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO sda.dbschema_version VALUES(sourcever+1, now(), changes);

    -- add new permissions
    GRANT SELECT ON sda.file_event_log TO finalize;
    GRANT SELECT ON sda.file_event_log TO ingest;
    GRANT SELECT ON sda.file_event_log TO verify;

    -- New ingestion specific functions
    CREATE FUNCTION sda.set_archived(file_uuid UUID, corr_id UUID, file_path TEXT, file_size BIGINT, inbox_checksum_value TEXT, inbox_checksum_type TEXT)
    RETURNS void AS $set_archived$
    BEGIN
        UPDATE sda.files SET archive_file_path = file_path, archive_file_size = file_size WHERE id = file_uuid;

        INSERT INTO sda.checksums(file_id, checksum, type, source)
        VALUES(file_uuid, inbox_checksum_value, upper(inbox_checksum_type)::sda.checksum_algorithm, upper('UPLOADED')::sda.checksum_source);

        INSERT INTO sda.file_event_log(file_id, event, correlation_id) VALUES(file_uuid, 'archived', corr_id);
    END;

    $set_archived$ LANGUAGE plpgsql;

    CREATE FUNCTION sda.set_verified(file_uuid UUID, corr_id UUID, archive_checksum TEXT, archive_checksum_type TEXT, decrypted_size BIGINT, decrypted_checksum TEXT, decrypted_checksum_type TEXT)
    RETURNS void AS $set_verified$
    BEGIN
        UPDATE sda.files SET decrypted_file_size = decrypted_size WHERE id = file_uuid;

        INSERT INTO sda.checksums(file_id, checksum, type, source)
        VALUES(file_uuid, archive_checksum, upper(archive_checksum_type)::sda.checksum_algorithm, upper('ARCHIVED')::sda.checksum_source);

        INSERT INTO sda.checksums(file_id, checksum, type, source)
        VALUES(file_uuid, decrypted_checksum, upper(decrypted_checksum_type)::sda.checksum_algorithm, upper('UNENCRYPTED')::sda.checksum_source);

        INSERT INTO sda.file_event_log(file_id, event, correlation_id) VALUES(file_uuid, 'verified', corr_id);
    END;

    $set_verified$ LANGUAGE plpgsql;

  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$
