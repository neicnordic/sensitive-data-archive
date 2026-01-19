
DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 23;
  changes VARCHAR := 'Grant SELECT on sda.file_dataset to mapper, finalize, ingest, sync, and verify';
BEGIN
  IF (select max(version) from sda.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO sda.dbschema_version VALUES(sourcever+1, now(), changes);

    GRANT SELECT ON sda.file_dataset TO mapper;
    GRANT SELECT ON sda.file_dataset TO finalize;
    GRANT SELECT ON sda.file_dataset TO ingest;
    GRANT SELECT ON sda.file_dataset TO sync;
    GRANT SELECT ON sda.file_dataset TO verify;

  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$
