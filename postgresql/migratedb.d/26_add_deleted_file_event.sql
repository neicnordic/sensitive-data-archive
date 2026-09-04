
DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 25;
  changes VARCHAR := 'Add deleted file event';
BEGIN
  IF (select max(version) from sda.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO sda.dbschema_version VALUES(sourcever+1, now(), changes);

    INSERT INTO sda.file_events(id, title, description)
    VALUES (3, 'deleted', 'File has been deleted from the inbox storage by the user');

  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$
