
DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 6;
  changes VARCHAR := 'Add permissions to mapper to files ';
BEGIN
-- No explicit transaction handling here, this all happens in a transaction
-- automatically
  IF (select max(version) from sda.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO sda.dbschema_version VALUES(sourcever+1, now(), changes);

    GRANT INSERT ON sda.file_event_log TO mapper;
    GRANT UPDATE ON local_ega.files TO mapper;
    GRANT USAGE, SELECT ON SEQUENCE sda.file_event_log_id_seq TO mapper;
    
  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$


