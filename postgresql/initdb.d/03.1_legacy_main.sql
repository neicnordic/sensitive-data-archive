--
-- This schema acts as a wrapper to the legacy local_ega schema.
--

CREATE SCHEMA local_ega;

SET search_path TO local_ega;

-- the version table needs to be here for the migrations to trigger properly
CREATE VIEW dbschema_version AS
    SELECT
        version,
        applied,
        description
    FROM sda.dbschema_version;

-- this version still uses the storage type
CREATE TYPE storage AS ENUM ('S3', 'POSIX');

-- file status is replaced by file events
CREATE VIEW status AS
    SELECT
        id,
        UPPER(title) AS code,
        description
    FROM sda.file_events;

-- archive encryption is a TEXT field in the sda schema.
CREATE VIEW archive_encryption AS
    SELECT
        DISTINCT encryption_method AS mode,
        '' AS description
    FROM sda.files;

-- table for matching old (integer) id's to new uuid id's
CREATE TABLE IF NOT EXISTS main_to_files (
    main_id SERIAL PRIMARY KEY,
    files_id UUID
);

-- create trigger function for making sure that the main_to_files table stays
-- updated.
CREATE FUNCTION files_insert()
RETURNS TRIGGER AS $files_insert$
BEGIN
    INSERT INTO
        local_ega.main_to_files(files_id)
        VALUES(NEW.id);
    RETURN NEW;
END;
$files_insert$ LANGUAGE plpgsql;

CREATE TRIGGER main_to_files_insert
    AFTER INSERT ON sda.files
    FOR EACH ROW
    EXECUTE PROCEDURE files_insert();

-- Translation table for file status values
CREATE TABLE status_translation (
    id SERIAL PRIMARY KEY,
    legacy_value TEXT,
    new_value TEXT
);

INSERT INTO status_translation (legacy_value, new_value)
    VALUES  ('INIT', 'uploaded'),
            ('IN_INGESTION', 'submitted'),
            ('COMPLETED', 'verified');

-- main is where most of the data is, but it matches pretty well with the new
-- files table.
CREATE VIEW main AS
    SELECT
        m.main_id AS id,
        stable_id,
        COALESCE((SELECT legacy_value FROM local_ega.status_translation WHERE new_value = l.event), UPPER(l.event)) AS status,
        submission_file_path AS submission_file_path,
        substring(submission_file_path from '\.(.*)') AS submission_file_extension,
        sc.checksum AS submission_file_calculated_checksum,
        sc.type AS submission_file_calculated_checksum_type,
        submission_file_size,
        submission_user,
        archive_file_path AS archive_file_reference,
        NULL AS archive_file_type,
        archive_file_size,
        ac.checksum AS archive_file_checksum,
        ac.type AS archive_file_checksum_type,
        decrypted_file_size,
        uc.checksum AS decrypted_file_checksum,
        uc.type AS decrypted_file_checksum_type,
        encryption_method,
        1 AS version,
        header,
        created_by,
        last_modified_by,
        created_at,
        last_modified
    FROM sda.files f
    JOIN main_to_files m
    ON f.id = m.files_id
    LEFT JOIN (SELECT file_id,
                      (ARRAY_AGG(event ORDER BY started_at DESC))[1] AS event
                FROM sda.file_event_log
                GROUP BY file_id) l
    ON f.id = l.file_id
    LEFT JOIN (SELECT file_id, checksum, type
            FROM sda.checksums
           WHERE source = 'UPLOADED') sc
    ON f.id = sc.file_id
    LEFT JOIN (SELECT file_id, checksum, type
            FROM sda.checksums
           WHERE source = 'ARCHIVED') ac
    ON f.id = ac.file_id
    LEFT JOIN (SELECT file_id, checksum, type
            FROM sda.checksums
           WHERE source = 'UNENCRYPTED') uc
    ON f.id = uc.file_id;

-- Create a trigger to hijack inserts on main
CREATE FUNCTION main_insert()
RETURNS TRIGGER AS $main_insert$
    #variable_conflict use_column
    DECLARE
        file_id  UUID;
    BEGIN
        -- insert bulk data into sda.files
        INSERT INTO sda.files (
            stable_id,
            submission_user,
            submission_file_path,
            submission_file_size,
            archive_file_path,
            archive_file_size,
            decrypted_file_size,
            backup_path,
            header,
            encryption_method,
            created_at,
            created_by,
            last_modified,
            last_modified_by
            ) VALUES (
                NEW.stable_id,
                NEW.submission_user,
                NEW.submission_file_path,
                NEW.submission_file_size,
                COALESCE(NEW.archive_file_reference, ''),
                NEW.archive_file_size,
                NEW.decrypted_file_size,
                NULL,
                NEW.header,
                NEW.encryption_method,
                COALESCE(NEW.created_at, clock_timestamp()),
                COALESCE(NEW.created_by, CURRENT_USER),
                COALESCE(NEW.last_modified, clock_timestamp()),
                COALESCE(NEW.last_modified_by, CURRENT_USER)
            )
            RETURNING id INTO file_id;

        -- update status names if needed
        IF NEW.status IN (SELECT legacy_value FROM local_ega.status_translation)
        THEN
            SELECT new_value
              FROM local_ega.status_translation
             WHERE legacy_value = NEW.status
              INTO NEW.status;
        END IF;
        NEW.status = lower(NEW.status);

        -- if we have a status, create a log event
        IF NEW.status IS NOT NULL
        THEN
            INSERT INTO sda.file_event_log (file_id, event, user_id)
            VALUES (file_id, NEW.status, NEW.submission_user);
        END IF;

        -- if there are checksums, insert them into the sda.checksums table

        IF NEW.submission_file_calculated_checksum IS NOT NULL
        THEN
            INSERT INTO sda.checksums (file_id, checksum, type, source)
            VALUES (file_id,
                    NEW.submission_file_calculated_checksum,
                    NEW.submission_file_calculated_checksum_type,
                    'UPLOADED');
        END IF;

        IF NEW.archive_file_checksum IS NOT NULL
        THEN
            INSERT INTO sda.checksums (file_id, checksum, type, source)
            VALUES (file_id,
                    NEW.archive_file_checksum,
                    NEW.archive_file_checksum_type,
                    'ARCHIVED');
        END IF;

        IF NEW.decrypted_file_checksum IS NOT NULL
        THEN
            INSERT INTO sda.checksums (file_id, checksum, type, source)
            VALUES (file_id,
                    NEW.decrypted_file_checksum,
                    NEW.decrypted_file_checksum_type,
                    'UNENCRYPTED');
        END IF;

        -- update id
        SELECT main_id
            FROM local_ega.main_to_files
            WHERE files_id = file_id
            INTO NEW.id;

        RETURN NEW;
    END;
$main_insert$ LANGUAGE plpgsql;

CREATE TRIGGER main_insert_trigger
    INSTEAD OF INSERT ON local_ega.main
    FOR EACH ROW EXECUTE PROCEDURE main_insert();

CREATE FUNCTION main_update()
RETURNS TRIGGER AS $main_update$
    #variable_conflict use_column
    DECLARE
        file_id  UUID;
    BEGIN
        SELECT files_id
            FROM local_ega.main_to_files
            WHERE main_id = OLD.id
            INTO file_id;

        -- insert bulk data into sda.files
        UPDATE sda.files SET
            stable_id = NEW.stable_id,
            submission_user = NEW.submission_user,
            submission_file_path = NEW.submission_file_path,
            submission_file_size = NEW.submission_file_size,
            archive_file_path = COALESCE(NEW.archive_file_reference, ''),
            archive_file_size = NEW.archive_file_size,
            decrypted_file_size = NEW.decrypted_file_size,
            header = NEW.header,
            encryption_method = NEW.encryption_method,
            created_at = COALESCE(NEW.created_at, clock_timestamp()),
            created_by = COALESCE(NEW.created_by, CURRENT_USER),
            last_modified = COALESCE(NEW.last_modified, clock_timestamp()),
            last_modified_by = COALESCE(NEW.last_modified_by, CURRENT_USER)
            WHERE id = file_id;

        -- update status names if needed
        IF NEW.status IN (SELECT legacy_value FROM local_ega.status_translation)
        THEN
            SELECT new_value
              FROM local_ega.status_translation
             WHERE legacy_value = NEW.status
              INTO NEW.status;
        END IF;
        NEW.status = lower(NEW.status);

        -- if we have a status, create a log event
        IF NEW.status IS NOT NULL
        THEN
            INSERT INTO sda.file_event_log (file_id, event, user_id)
            VALUES (file_id, NEW.status, NEW.submission_user);
        END IF;

        -- if there are checksums, insert them into the sda.checksums table

        IF NEW.submission_file_calculated_checksum IS NOT NULL
        THEN
            INSERT INTO sda.checksums (file_id, checksum, type, source)
            VALUES (file_id,
                    NEW.submission_file_calculated_checksum,
                    NEW.submission_file_calculated_checksum_type,
                    'UPLOADED')
            ON CONFLICT ON CONSTRAINT unique_checksum DO UPDATE SET checksum = NEW.submission_file_calculated_checksum;
        END IF;

        IF NEW.archive_file_checksum IS NOT NULL
        THEN
            INSERT INTO sda.checksums (file_id, checksum, type, source)
            VALUES (file_id,
                    NEW.archive_file_checksum,
                    NEW.archive_file_checksum_type,
                    'ARCHIVED')
            ON CONFLICT ON CONSTRAINT unique_checksum DO UPDATE SET checksum = NEW.archive_file_checksum;
        END IF;

        IF NEW.decrypted_file_checksum IS NOT NULL
        THEN
            INSERT INTO sda.checksums (file_id, checksum, type, source)
            VALUES (file_id,
                    NEW.decrypted_file_checksum,
                    NEW.decrypted_file_checksum_type,
                    'UNENCRYPTED')
            ON CONFLICT ON CONSTRAINT unique_checksum DO UPDATE SET checksum = NEW.decrypted_file_checksum;
        END IF;

        -- update id
        SELECT main_id
            FROM local_ega.main_to_files
            WHERE files_id = file_id
            INTO NEW.id;

        RETURN NEW;
    END;
$main_update$ LANGUAGE plpgsql;

CREATE TRIGGER main_update_trigger
    INSTEAD OF UPDATE ON local_ega.main
    FOR EACH ROW EXECUTE PROCEDURE main_update();

-- ##################################################
--                      ERRORS
-- ##################################################
CREATE VIEW local_ega.main_errors AS
    SELECT
        e.id AS id,
        e.details->>'active' AS active,
        mtf.main_id AS file_id,
        e.details->>'hostname' AS hostname,
        e.error AS error_type,
        e.details->>'message' AS msg,
        e.details->>'from_user' AS from_user,
        e.started_at AS occured_at
    FROM sda.file_event_log e
    JOIN local_ega.main_to_files mtf
      ON e.file_id = mtf.files_id
    WHERE e.event = 'error';

-- ##################################################
--         Data-In View
-- ##################################################
--
CREATE VIEW local_ega.files AS
SELECT id,
       submission_user                          AS elixir_id,
       submission_file_path                     AS inbox_path,
       submission_file_size                     AS inbox_filesize,
       submission_file_calculated_checksum      AS inbox_file_checksum,
       submission_file_calculated_checksum_type AS inbox_file_checksum_type,
       status,
       archive_file_reference                   AS archive_path,
       archive_file_type                        AS archive_type,
       archive_file_size                        AS archive_filesize,
       archive_file_checksum                    AS archive_file_checksum,
       archive_file_checksum_type               AS archive_file_checksum_type,
       decrypted_file_size                      AS decrypted_file_size,
       decrypted_file_checksum                  AS decrypted_file_checksum,
       decrypted_file_checksum_type             AS decrypted_file_checksum_type,
       stable_id,
       header,  -- Crypt4gh specific
       version,
       created_at,
       last_modified
FROM local_ega.main;

-- Insert into sda.files
CREATE FUNCTION insert_file(inpath        TEXT,
                            eid           TEXT)
RETURNS INT AS $insert_file$
    #variable_conflict use_column
    DECLARE
        file_id  UUID;
        main_id  INT;
        file_ext TEXT;
    BEGIN
        -- Make a new insertion
    INSERT INTO sda.files ( submission_file_path,
                            submission_user,
                            encryption_method) -- hard-code the archive_encryption
    VALUES(inpath,eid,'CRYPT4GH') RETURNING id
    INTO file_id;

    INSERT INTO sda.file_event_log (
        file_id,
        event,
        user_id
    ) VALUES (file_id, 'uploaded', eid);

    SELECT main_id
      FROM local_ega.main_to_files
     WHERE files_id = file_id
      INTO main_id;

    RETURN main_id;
    END;
$insert_file$ LANGUAGE plpgsql;

-- Flag as READY, and mark older ingestion as deprecated (to clean up)
CREATE FUNCTION finalize_file(inpath        TEXT,
                              eid           TEXT,
                              checksum      TEXT,
                              checksum_type VARCHAR,
                              sid           TEXT)
    RETURNS void AS $finalize_file$
    #variable_conflict use_column
    BEGIN
    UPDATE local_ega.files
    SET status = 'ready',
        stable_id = sid
    WHERE archive_file_checksum = checksum AND
          archive_file_checksum_type = upper(checksum_type)::sda.checksum_algorithm AND
          elixir_id = eid AND
          inbox_path = inpath AND
          status IN ('COMPLETED', 'BACKED UP');
    END;
$finalize_file$ LANGUAGE plpgsql;

-- If the entry is marked disabled, it says disabled. No data race here.
CREATE FUNCTION is_disabled(fid INT)
RETURNS boolean AS $is_disabled$
#variable_conflict use_column
BEGIN
   RETURN EXISTS(SELECT 1 FROM local_ega.files WHERE id = fid AND status = 'DISABLED');
END;
$is_disabled$ LANGUAGE plpgsql;


-- Just showing the current/active errors
CREATE VIEW local_ega.errors AS
SELECT id, file_id, hostname, error_type, msg, from_user, occured_at
FROM local_ega.main_errors
WHERE active::BOOLEAN = TRUE;

CREATE FUNCTION insert_error(fid        INT,
                             h          TEXT,
                             etype      TEXT,
                             msg        TEXT,
                             from_user  TEXT)
    RETURNS void AS $insert_error$
    BEGIN
        INSERT INTO sda.file_event_log(
            file_id,
            event,
            user_id,
            details,
            error
        ) VALUES (
            (SELECT files_id FROM local_ega.main_to_files WHERE main_id = fid),
            'error',
            from_user,
            concat('{"active": true',
                   ', "hostname": "', h,
                   '", "from_user": "', from_user,
                   '", "message": "', msg, '"}')::json,
            etype
        );
    END;
$insert_error$ LANGUAGE plpgsql;


-- ##################################################
--              Session Keys Checksums
-- ##################################################
-- To keep track of already used session keys,
-- we record their checksum
CREATE TABLE local_ega.session_key_checksums_sha256 (
       session_key_checksum      VARCHAR(128) NOT NULL, PRIMARY KEY(session_key_checksum), UNIQUE (session_key_checksum),
       session_key_checksum_type sda.checksum_algorithm,
       file_id                   INTEGER NOT NULL REFERENCES local_ega.main_to_files(main_id) ON DELETE CASCADE
);


-- Returns if the session key checksums are already found in the database
CREATE FUNCTION check_session_keys_checksums_sha256(checksums text[]) --local_ega.session_key_checksums.session_key_checksum%TYPE []
    RETURNS boolean AS $check_session_keys_checksums_sha256$
    #variable_conflict use_column
    BEGIN
    RETURN EXISTS(SELECT 1
                      FROM local_ega.session_key_checksums_sha256 sk
                  INNER JOIN local_ega.files f
              ON f.id = sk.file_id
              WHERE (f.status <> 'ERROR' AND f.status <> 'DISABLED') AND -- no data-race on those values
                    sk.session_key_checksum = ANY(checksums));
    END;
$check_session_keys_checksums_sha256$ LANGUAGE plpgsql;

-- ##########################################################################
--           For data-out
-- ##########################################################################

-- View on the archive files
CREATE VIEW local_ega.archive_files AS
SELECT id                          AS file_id
     , stable_id                   AS stable_id
     , archive_file_reference      AS archive_path
     , archive_file_type           AS archive_type
     , archive_file_size           AS archive_filesize
     , archive_file_checksum       AS archive_file_checksum
     , archive_file_checksum_type  AS archive_file_checksum_type
     , header                      AS header
     , version                     AS version
FROM local_ega.main
WHERE status = 'READY';
