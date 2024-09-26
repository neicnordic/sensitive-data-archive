DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 12;
  changes VARCHAR := 'Create API user';
BEGIN
-- No explicit transaction handling here, this all happens in a transaction
-- automatically
  IF (select max(version) from sda.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO sda.dbschema_version VALUES(sourcever+1, now(), changes);

    -- Temporary function for creating roles if they do not already exist.
    CREATE FUNCTION create_role_if_not_exists(role_name NAME) RETURNS void AS $created$
    BEGIN
        IF EXISTS (
            SELECT FROM pg_catalog.pg_roles
            WHERE  rolname = role_name) THEN
                RAISE NOTICE 'Role "%" already exists. Skipping.', role_name;
        ELSE
            BEGIN
                EXECUTE format('CREATE ROLE %I', role_name);
            EXCEPTION
                WHEN duplicate_object THEN
                    RAISE NOTICE 'Role "%" was just created by a concurrent transaction. Skipping.', role_name;
            END;
        END IF;
    END;
    $created$ LANGUAGE plpgsql;

    PERFORM create_role_if_not_exists('api');
    GRANT USAGE ON SCHEMA sda TO api;
    GRANT SELECT ON sda.files TO api;
    GRANT SELECT ON sda.file_event_log TO api;
    GRANT SELECT ON sda.file_dataset TO api;
    GRANT SELECT  ON sda.checksums TO api;
    GRANT SELECT, INSERT, UPDATE ON sda.encryption_keys TO api;
    GRANT USAGE ON SCHEMA local_ega TO api;
    GRANT USAGE ON SCHEMA local_ega_ebi TO api;
    GRANT SELECT ON local_ega.files TO api;
    GRANT SELECT ON local_ega_ebi.file TO api;
    GRANT SELECT ON local_ega_ebi.file_dataset TO api;

    GRANT base TO api, download, inbox, ingest, finalize, mapper, verify;

    -- Drop temporary user creation function
    DROP FUNCTION create_role_if_not_exists;

  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$
