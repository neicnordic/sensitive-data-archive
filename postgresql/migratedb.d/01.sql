
DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 0;
  changes VARCHAR := 'Noop schema migration, can be used as template';
BEGIN
-- No explicit transaction handling here, this all happens in a transaction 
-- automatically 
  IF (select max(version) from local_ega.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO local_ega.dbschema_version VALUES(sourcever+1, now(), changes);
  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$


