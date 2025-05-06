DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 16;
  changes VARCHAR := 'Add submission user to constraint';
BEGIN
  IF (select max(version) from sda.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO sda.dbschema_version VALUES(sourcever+1, now(), changes);

    ALTER TABLE sda.files DROP CONSTRAINT unique_ingested;
    ALTER TABLE sda.files ADD CONSTRAINT unique_ingested UNIQUE(submission_file_path, archive_file_path, submission_user);

  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$