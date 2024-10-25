DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 12;
  changes VARCHAR := 'Add userinfo';
BEGIN
  IF (select max(version) from sda.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO sda.dbschema_version VALUES(sourcever+1, now(), changes);

    CREATE TABLE IF NOT EXISTS sda.userinfo (
        id          TEXT PRIMARY KEY,
        name        TEXT,
        email       TEXT,
        groups      TEXT[]
    );

    PERFORM create_role_if_not_exists('auth');
    GRANT USAGE ON SCHEMA sda TO auth;
    GRANT SELECT, INSERT, UPDATE ON sda.userinfo TO auth;

    GRANT base TO api, download, inbox, ingest, finalize, mapper, verify, auth;
  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$