DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 11;
  changes VARCHAR := 'Add key hash';
BEGIN
  IF (select max(version) from sda.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO sda.dbschema_version VALUES(sourcever+1, now(), changes);

    CREATE TABLE IF NOT EXISTS sda.encryption_keys (
        key_hash          TEXT PRIMARY KEY,
        created_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT clock_timestamp(),
        deprecated_at     TIMESTAMP WITH TIME ZONE,
        description       TEXT
    );

    ALTER TABLE sda.files ADD COLUMN IF NOT EXISTS key_hash TEXT,
    ADD CONSTRAINT fk_files_key_hash FOREIGN KEY (key_hash) REFERENCES sda.encryption_keys(key_hash);

  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$