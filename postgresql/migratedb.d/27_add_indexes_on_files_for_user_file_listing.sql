
DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 26;
  changes VARCHAR := 'Add indexes on files for listing a user''s files by path prefix and by id';
BEGIN
  IF (select max(version) from sda.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO sda.dbschema_version VALUES(sourcever+1, now(), changes);

    -- A path prefix lookup is a range scan, and a btree range only matches a
    -- prefix when the index compares bytes, which the database's default
    -- collation (e.g. en_US.utf8) does not.
    --
    -- Each index blocks writes to sda.files while it is built (reads are not
    -- blocked). Built CONCURRENTLY on a table of 4.7M rows / 12 GB, the first
    -- index took 80 s; a plain build is usually faster. To avoid the write
    -- block on a live database, create the indexes beforehand with
    -- CREATE INDEX CONCURRENTLY under the same names; this migration then
    -- skips them.
    CREATE INDEX IF NOT EXISTS files_submission_user_submission_file_path_c_idx
        ON sda.files(submission_user, submission_file_path COLLATE "C");

    -- Lets a listing without a path prefix read a user's files in id order and
    -- stop after one page, instead of reading and sorting all of them.
    CREATE INDEX IF NOT EXISTS files_submission_user_id_idx
        ON sda.files(submission_user, id);

  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$
