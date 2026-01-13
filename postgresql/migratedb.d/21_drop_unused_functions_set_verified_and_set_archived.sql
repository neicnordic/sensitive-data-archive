
DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 20;
  changes VARCHAR := 'Drop functions set_verified, and set_archived';
BEGIN
  IF (select max(version) from sda.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO sda.dbschema_version VALUES(sourcever+1, now(), changes);


    -- Drop set_verified func, as not in use
    DROP FUNCTION IF EXISTS sda.set_verified;

    -- Drop set_archived func, as not in use
    DROP FUNCTION IF EXISTS sda.set_archived;

ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$
