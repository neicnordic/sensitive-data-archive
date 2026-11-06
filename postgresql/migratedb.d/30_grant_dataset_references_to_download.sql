DO
$$
DECLARE
  -- The version we know how to migrate from. After a successful migration,
  -- the database will no longer be at this version.
  sourcever INTEGER := 29;
  changes VARCHAR := 'Grant download access to dataset_references';
BEGIN
  IF (SELECT max(version) FROM sda.dbschema_version) = sourcever THEN
    RAISE NOTICE 'Doing migration from schema version % to %',
      sourcever, sourcever + 1;
    RAISE NOTICE 'Changes: %', changes;

    IF EXISTS (
      SELECT FROM pg_catalog.pg_roles
      WHERE rolname = 'download'
    ) THEN
      GRANT SELECT ON sda.dataset_references TO download;
    ELSE
      RAISE NOTICE 'Role "download" does not exist. Skipping grant.';
    END IF;

    INSERT INTO sda.dbschema_version
    VALUES (sourcever + 1, now(), changes);

    RAISE NOTICE 'Migration completed successfully';
  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping',
      sourcever, sourcever + 1;
  END IF;
END
$$;