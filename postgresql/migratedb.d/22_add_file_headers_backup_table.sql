DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 21;
  changes VARCHAR := 'Add file_headers_backup table for key rotation safekeeping';
BEGIN
  IF (SELECT max(version) FROM sda.dbschema_version) = sourcever THEN
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    
    INSERT INTO sda.dbschema_version VALUES(sourcever+1, now(), changes);

    CREATE TABLE IF NOT EXISTS sda.file_headers_backup (
        file_id     UUID REFERENCES sda.files(id) PRIMARY KEY,
        header      TEXT NOT NULL,
        key_hash    TEXT REFERENCES sda.encryption_keys(key_hash),
        backup_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT clock_timestamp()
    );


    -- Grant permissions to the rotatekey role
    GRANT INSERT, SELECT ON sda.file_headers_backup TO rotatekey;

    RAISE NOTICE 'Migration to version % completed successfully.', sourcever+1;

  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$;