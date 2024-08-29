DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 16;
  changes VARCHAR := 'Update SYNC user permissions';
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

    PERFORM create_role_if_not_exists('sync');
    GRANT SELECT ON sda.file_dataset TO sync;

    GRANT base TO sync;

    -- Drop temporary user creation function
    DROP FUNCTION create_role_if_not_exists;

  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$