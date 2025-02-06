
DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 16;
  changes VARCHAR := 'Remove user ids from filepaths';
BEGIN
  IF (select max(version) from sda.dbschema_version) = sourcever then
    RAISE NOTICE 'Changes: %', changes;

    UPDATE sda.files
    SET submission_file_path = regexp_replace(submission_file_path, '^[^/]*/', '');    

  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$