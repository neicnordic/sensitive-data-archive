
DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 18;
  changes VARCHAR := 'Create new indexes on files and file_event_log tables, and new generated column submission_file_root_dir on files table';
BEGIN
  IF (select max(version) from sda.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO sda.dbschema_version VALUES(sourcever+1, now(), changes);

    CREATE INDEX file_event_log_file_id_started_at_idx ON sda.file_event_log(file_id, started_at);

    CREATE INDEX files_submission_user_submission_file_path_idx
        ON sda.files(submission_user, submission_file_path);

  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$
