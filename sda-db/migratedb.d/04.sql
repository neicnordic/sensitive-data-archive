
DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 3;
  changes VARCHAR := 'Refactored schema';
BEGIN
-- No explicit transaction handling here, this all happens in a transaction
-- automatically
  IF (select max(version) from local_ega.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO local_ega.dbschema_version VALUES(sourcever+1, now(), changes);

    ---------------------------------------------------------------------------
    -- Create refactored schema                                              --
    ---------------------------------------------------------------------------

    CREATE SCHEMA IF NOT EXISTS sda;
    SET search_path TO sda;

    CREATE TYPE checksum_algorithm AS ENUM ('MD5', 'SHA256', 'SHA384', 'SHA512');
    CREATE TYPE encryption_algorithm AS ENUM ('CRYPT4GH', 'PGP', 'AES');
    CREATE TYPE checksum_source AS ENUM ('UPLOADED', 'ARCHIVED', 'UNENCRYPTED');

    CREATE TABLE  dbschema_version (
        version          INTEGER PRIMARY KEY,
        applied          TIMESTAMP WITH TIME ZONE,
        description      VARCHAR(1024)
    );

    CREATE TABLE datasets (
        id                  SERIAL PRIMARY KEY,
        stable_id           TEXT UNIQUE,
        title               TEXT,
        description         TEXT
    );

    CREATE TABLE files (
        id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
        stable_id            TEXT UNIQUE,

        submission_user      TEXT,
        submission_file_path TEXT DEFAULT '' NOT NULL,
        submission_file_size BIGINT,
        archive_file_path    TEXT DEFAULT '' NOT NULL,
        archive_file_size    BIGINT,
        decrypted_file_size  BIGINT,
        backup_path          TEXT,

        header               TEXT,
        encryption_method    TEXT,

        created_by           NAME DEFAULT CURRENT_USER,
        last_modified_by     NAME DEFAULT CURRENT_USER,
        created_at           TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT clock_timestamp(),
        last_modified        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT clock_timestamp(),

        CONSTRAINT unique_ingested UNIQUE(submission_file_path, archive_file_path)
    );

    CREATE TABLE checksums (
        id                  SERIAL PRIMARY KEY,
        file_id             UUID REFERENCES files(id),
        checksum            TEXT,
        type                checksum_algorithm,
        source              checksum_source,
        CONSTRAINT unique_checksum UNIQUE(file_id, type, source)
    );

    CREATE TABLE dataset_references (
        id                  SERIAL PRIMARY KEY,
        dataset_id          INT REFERENCES datasets(id),
        reference_id        TEXT NOT NULL,
        reference_scheme    TEXT NOT NULL, -- E.g. “DOI” or “EGA”
        created_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT clock_timestamp(),
        expired_at          TIMESTAMP
    );

    CREATE TABLE file_references (
        file_id             UUID REFERENCES files(id),
        reference_id        TEXT NOT NULL,
        reference_scheme    TEXT NOT NULL, -- E.g. “DOI” or “EGA”
        created_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT clock_timestamp(),
        expired_at          TIMESTAMP
    );

    CREATE TABLE file_dataset (
        id                  SERIAL PRIMARY KEY,
        file_id             UUID REFERENCES files(id) NOT NULL,
        dataset_id          INT REFERENCES datasets(id) NOT NULL,
        CONSTRAINT unique_file_dataset UNIQUE(file_id, dataset_id)
    );

    CREATE TABLE file_events (
        id                  SERIAL PRIMARY KEY,
        title               VARCHAR(64) UNIQUE,
        description         TEXT
    );

    INSERT INTO file_events(id,title,description)
    VALUES ( 5, 'registered'  , 'Upload to the inbox has started'),
           (10, 'uploaded'    , 'Upload to the inbox has finished'),
           (20, 'submitted'   , 'User has submitted the file to the archive'),
           (30, 'ingested'    , 'File information has been added to the database'),
           (40, 'archived'    , 'File has been moved to the archive'),
           (50, 'verified'    , 'Checksums have been verified in the archived file'),
           (60, 'backed up'   , 'File has been backed up'),
           (70, 'ready'       , 'File is ready for access requests'),
           (80, 'downloaded'  , 'Downloaded by user'),
           ( 0, 'error'       , 'An Error occurred, check the error table'),
           ( 1, 'disabled'    , 'Disables the file for all actions'),
           ( 2, 'enabled'     , 'Reenables a disabled file');

    CREATE TABLE file_event_log (
        id                  SERIAL PRIMARY KEY,
        file_id             UUID REFERENCES files(id),
        event               TEXT REFERENCES file_events(title),
        user_id             TEXT,
        details             JSONB,
        message             JSONB,
        started_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT clock_timestamp(),
        finished_at         TIMESTAMP,
        success             BOOLEAN,
        error               TEXT
    );

    ---------------------------------------------------------------------------
    -- Rename old local_ega schema as it's almost completely replaced and    --
    -- create the new local_ega schema.                                      --
    ---------------------------------------------------------------------------

    ALTER SCHEMA local_ega RENAME TO local_ega_transfer_temp;

    CREATE SCHEMA local_ega;

    SET search_path TO local_ega;

    CREATE VIEW dbschema_version AS
        SELECT
            version,
            applied,
            description
        FROM sda.dbschema_version;

    CREATE TYPE storage AS ENUM ('S3', 'POSIX');

    CREATE VIEW status AS
        SELECT
            id,
            UPPER(title) AS code,
            description
        FROM sda.file_events;

    CREATE VIEW archive_encryption AS
        SELECT
            unnest(enum_range(null::sda.encryption_algorithm)) AS mode,
            '' AS description;

    CREATE TABLE IF NOT EXISTS main_to_files (
        main_id SERIAL PRIMARY KEY,
        files_id UUID
    );

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

    CREATE TABLE status_translation (
        id SERIAL PRIMARY KEY,
        legacy_value TEXT,
        new_value TEXT
    );

    INSERT INTO status_translation (legacy_value, new_value)
        VALUES  ('INIT', 'uploaded'),
                ('IN_INGESTION', 'submitted'),
                ('COMPLETED', 'verified');

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

    CREATE FUNCTION main_insert()
    RETURNS TRIGGER AS $main_insert$
        #variable_conflict use_column
        DECLARE
            file_id  UUID;
        BEGIN
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

            -- make sure the new main_id is the same as in the old database
            IF NEW.id IS NOT NULL
            THEN
                UPDATE local_ega.main_to_files
                SET main_id = NEW.id WHERE files_id = file_id;

                -- update sequence in case the new id was larger than the old max
                PERFORM setval('main_to_files_main_id_seq', (SELECT MAX(main_id) FROM main_to_files), true);
            END IF;

            IF NEW.status IN (SELECT legacy_value FROM local_ega.status_translation)
            THEN
                SELECT new_value
                FROM local_ega.status_translation
                WHERE legacy_value = NEW.status
                INTO NEW.status;
            END IF;
            NEW.status = lower(NEW.status);

            IF NEW.status IS NOT NULL
            THEN
                INSERT INTO sda.file_event_log (file_id, event, user_id)
                VALUES (file_id, NEW.status, NEW.submission_user);
            END IF;

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

            IF NEW.status IN (SELECT legacy_value FROM local_ega.status_translation)
            THEN
                SELECT new_value
                FROM local_ega.status_translation
                WHERE legacy_value = NEW.status
                INTO NEW.status;
            END IF;
            NEW.status = lower(NEW.status);

            IF NEW.status IS NOT NULL
            THEN
                INSERT INTO sda.file_event_log (file_id, event, user_id)
                VALUES (file_id, NEW.status, NEW.submission_user);
            END IF;

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
        header,
        version,
        created_at,
        last_modified
    FROM local_ega.main;

    CREATE FUNCTION insert_file(inpath        TEXT,
                                eid           TEXT)
    RETURNS INT AS $insert_file$
        #variable_conflict use_column
        DECLARE
            file_id  UUID;
            main_id  INT;
            file_ext TEXT;
        BEGIN

        INSERT INTO sda.files ( submission_file_path,
                                submission_user,
                                encryption_method)
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

    CREATE FUNCTION is_disabled(fid INT)
    RETURNS boolean AS $is_disabled$
    #variable_conflict use_column
    BEGIN
    RETURN EXISTS(SELECT 1 FROM local_ega.files WHERE id = fid AND status = 'DISABLED');
    END;
    $is_disabled$ LANGUAGE plpgsql;

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

    CREATE TABLE local_ega.session_key_checksums_sha256 (
        session_key_checksum      VARCHAR(128) NOT NULL, PRIMARY KEY(session_key_checksum), UNIQUE (session_key_checksum),
        session_key_checksum_type sda.checksum_algorithm,
        file_id                   INTEGER NOT NULL REFERENCES local_ega.main_to_files(main_id) ON DELETE CASCADE
    );

    CREATE FUNCTION check_session_keys_checksums_sha256(checksums text[])
        RETURNS boolean AS $check_session_keys_checksums_sha256$
        #variable_conflict use_column
        BEGIN
        RETURN EXISTS(SELECT 1
                        FROM local_ega.session_key_checksums_sha256 sk
                    INNER JOIN local_ega.files f
                ON f.id = sk.file_id
                WHERE (f.status <> 'ERROR' AND f.status <> 'DISABLED') AND
                        sk.session_key_checksum = ANY(checksums));
        END;
    $check_session_keys_checksums_sha256$ LANGUAGE plpgsql;

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

    ---------------------------------------------------------------------------
    -- Move data from old to new tables. Since inserts are possible in the   --
    -- new local_ega schema as well, we can just move all the data directly. --
    ---------------------------------------------------------------------------

    INSERT INTO sda.dbschema_version
        SELECT  version,
                applied,
                description
        FROM local_ega_transfer_temp.dbschema_version;

    INSERT INTO local_ega.main
        SELECT  id,
                stable_id,
                status,
                submission_file_path,
                submission_file_extension,
                submission_file_calculated_checksum,
                (submission_file_calculated_checksum_type::TEXT)::sda.checksum_algorithm,
                submission_file_size,
                submission_user,
                archive_file_reference,
                archive_file_type,
                archive_file_size,
                archive_file_checksum,
                (archive_file_checksum_type::TEXT)::sda.checksum_algorithm,
                decrypted_file_size,
                decrypted_file_checksum,
                (decrypted_file_checksum_type::TEXT)::sda.checksum_algorithm,
                encryption_method::sda.encryption_algorithm,
                version,
                header,
                created_by,
                last_modified_by,
                created_at,
                last_modified
        FROM local_ega_transfer_temp.main;

    INSERT INTO local_ega.session_key_checksums_sha256
        SELECT  session_key_checksum,
                (session_key_checksum_type::TEXT)::sda.checksum_algorithm,
                file_id
        FROM local_ega_transfer_temp.session_key_checksums_sha256;

    ---------------------------------------------------------------------------
    -- Move datasets and errors that need to be massaged a bit.              --
    ---------------------------------------------------------------------------

    -- move dataset connections
    INSERT INTO sda.datasets
        (
            stable_id
        )
        SELECT DISTINCT dataset_stable_id
        FROM local_ega_ebi.filedataset;

    INSERT INTO sda.file_dataset
        (
            file_id,
            dataset_id
        )
        SELECT
            ( SELECT files_id FROM local_ega.main_to_files WHERE main_id = fd.file_id ),
            ( SELECT id FROM sda.datasets WHERE stable_id = fd.dataset_stable_id)
        FROM local_ega_ebi.filedataset fd;

    -- move errors to file log
    INSERT INTO sda.file_event_log
        (
            file_id,
            event,
            user_id,
            details,
            message,
            started_at,
            finished_at,
            success,
            error
        )
        SELECT
            (SELECT files_id FROM local_ega.main_to_files WHERE main_id = e.file_id) AS file_id,
            'error' AS event,
            NULL AS user_id,
            concat('{"active": ', active::text,
                   ', "hostname": "', hostname,
                   '", "from_user": ', from_user::text,
                   ', "message": "', msg, '"}')::json AS details,
            NULL as message,
            occured_at AS started_at,
            NULL as finished_at,
            FALSE as success,
            error_type AS error
        FROM local_ega.main_errors e;

    ---------------------------------------------------------------------------
    -- Update the downloads and ebi schemas.                                 --
    ---------------------------------------------------------------------------

    ALTER TABLE local_ega_download.requests DROP CONSTRAINT IF EXISTS requests_file_id_fkey;
    ALTER TABLE local_ega_download.requests ADD FOREIGN KEY(file_id) REFERENCES main_to_files(main_id) ON UPDATE cascade;
    ALTER TYPE local_ega_download.request_type ALTER ATTRIBUTE archive_file_checksum_type SET DATA TYPE sda.checksum_algorithm CASCADE;
    ALTER TYPE local_ega_download.request_type ALTER ATTRIBUTE archive_type SET DATA TYPE local_ega.storage CASCADE;

    ALTER TABLE local_ega_ebi.fileindexfile DROP CONSTRAINT IF EXISTS fileindexfile_file_id_fkey;
    ALTER TABLE local_ega_ebi.fileindexfile ADD FOREIGN KEY(file_id) REFERENCES main_to_files(main_id) ON UPDATE cascade;
    ALTER TABLE local_ega_ebi.fileindexfile ALTER COLUMN index_file_type TYPE local_ega.storage USING index_file_type::text::local_ega.storage;

    DROP VIEW local_ega_ebi.file_index_file;
    CREATE VIEW local_ega_ebi.file_index_file AS
        SELECT m.stable_id AS file_id, index_file_id FROM local_ega_ebi.fileindexfile fif
        INNER JOIN local_ega.main m ON fif.file_id=m.id;

    DROP VIEW IF EXISTS local_ega_ebi.file_dataset;
    DROP TABLE IF EXISTS local_ega_ebi.filedataset;

    DROP VIEW local_ega_ebi.file;
    CREATE VIEW local_ega_ebi.file AS
    SELECT stable_id                             AS file_id,
        archive_file_reference                   AS file_name,
        archive_file_reference                   AS file_path,
        reverse(split_part(reverse(submission_file_path::text), '/'::text, 1)) AS display_file_name,
        archive_file_size                        AS file_size,
        archive_file_size                        AS archive_file_size,
        NULL::text                               AS checksum,
        NULL::text                               AS checksum_type,
        NULL::text                               AS unencrypted_checksum,
        NULL::text                               AS unencrypted_checksum_type,
        archive_file_checksum                    AS archive_file_checksum,
        archive_file_checksum_type               AS archive_file_checksum_type,
        decrypted_file_size                      AS decrypted_file_size,
        decrypted_file_checksum                  AS decrypted_file_checksum,
        decrypted_file_checksum_type             AS decrypted_file_checksum_type,
        status                                   AS file_status,
        header                                   AS header
    FROM local_ega.main
    WHERE status = 'READY';

    CREATE VIEW local_ega_ebi.filedataset AS
        SELECT fd.id,
            mf.main_id AS file_id,
            d.stable_id AS dataset_stable_id
        FROM sda.file_dataset fd
        JOIN local_ega.main_to_files mf ON fd.file_id = mf.files_id
        JOIN sda.datasets d ON fd.dataset_id = d.id;

    -- Create triggers to hijack inserts and updates on filedataset
    CREATE FUNCTION filedataset_insert()
    RETURNS TRIGGER AS $filedataset_insert$
        #variable_conflict use_column
        BEGIN

            INSERT INTO sda.datasets (stable_id)
                VALUES (NEW.dataset_stable_id)
                ON CONFLICT DO NOTHING;

            INSERT INTO sda.file_dataset(file_id, dataset_id)
                VALUES ((SELECT files_id FROM local_ega.main_to_files WHERE main_id = NEW.file_id),
                        (SELECT id FROM sda.datasets WHERE stable_id = NEW.dataset_stable_id)
                        )
                ON CONFLICT DO NOTHING;

            RETURN NEW;
        END;
    $filedataset_insert$ LANGUAGE plpgsql;

    CREATE TRIGGER filedataset_insert_trigger
        INSTEAD OF INSERT ON local_ega_ebi.filedataset
        FOR EACH ROW EXECUTE PROCEDURE filedataset_insert();

    CREATE VIEW local_ega_ebi.file_dataset AS
        SELECT m.stable_id AS file_id,
               dataset_stable_id as dataset_id
          FROM local_ega_ebi.filedataset fd
    INNER JOIN local_ega.main m ON fd.file_id=m.id;

    ---------------------------------------------------------------------------
    -- Add triggers after inserting data so that the data isn't modified     --
    ---------------------------------------------------------------------------

    SET search_path TO sda;

    CREATE FUNCTION files_updated()
    RETURNS TRIGGER AS $files_updated$
    BEGIN
        NEW.last_modified = clock_timestamp();
        NEW.last_modified_by = CURRENT_USER;
        RETURN NEW;
    END;
    $files_updated$ LANGUAGE plpgsql;

    CREATE TRIGGER files_last_modified
        BEFORE UPDATE ON sda.files
        FOR EACH ROW
        EXECUTE PROCEDURE files_updated();

    CREATE FUNCTION sda.register_file(submission_file_path TEXT, submission_user TEXT)
    RETURNS TEXT AS $register_file$
    DECLARE
        file_ext TEXT;
        file_uuid UUID;
    BEGIN
        -- Upsert file information. we're not interested in restarted uploads so old
        -- overwritten files that haven't been ingested are updated instead of
        -- inserting a new row.
        INSERT INTO sda.files( submission_file_path, submission_user, encryption_method )
        VALUES( submission_file_path, submission_user, 'CRYPT4GH' )
            ON CONFLICT ON CONSTRAINT unique_ingested
            DO UPDATE SET submission_file_path = EXCLUDED.submission_file_path,
                        submission_user = EXCLUDED.submission_user,
                        encryption_method = EXCLUDED.encryption_method
            RETURNING id INTO file_uuid;

        -- We add a new event for every registration though, as this might help for
        -- debugging.
        INSERT INTO sda.file_event_log( file_id, event, user_id )
        VALUES (file_uuid, 'registered', submission_user);

        RETURN file_uuid;
    END;
    $register_file$ LANGUAGE plpgsql;

    ---------------------------------------------------------------------------
    -- Drop temporary schema                                                 --
    ---------------------------------------------------------------------------

    DROP SCHEMA local_ega_transfer_temp CASCADE;

    ---------------------------------------------------------------------------
    -- Set new permissions                                                   --
    ---------------------------------------------------------------------------

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

    PERFORM create_role_if_not_exists('base');
    GRANT USAGE ON SCHEMA sda TO base;
    GRANT USAGE ON SCHEMA local_ega TO base;
    GRANT SELECT ON sda.dbschema_version TO base;
    GRANT SELECT ON local_ega.dbschema_version TO base;

    PERFORM create_role_if_not_exists('ingest');
    GRANT USAGE ON SCHEMA sda TO ingest;
    GRANT INSERT ON sda.files TO ingest;
    GRANT SELECT ON sda.files TO ingest;
    GRANT INSERT ON sda.file_events TO ingest;
    GRANT UPDATE ON sda.files TO ingest;
    GRANT INSERT ON sda.checksums TO ingest;
    GRANT USAGE, SELECT ON SEQUENCE sda.checksums_id_seq TO ingest;
    GRANT INSERT ON sda.file_event_log TO ingest;
    GRANT USAGE, SELECT ON SEQUENCE sda.file_event_log_id_seq TO ingest;
    GRANT USAGE ON SCHEMA local_ega TO ingest;
    GRANT INSERT, SELECT ON local_ega.main TO ingest;
    GRANT SELECT ON local_ega.status_translation TO ingest;
    GRANT INSERT, SELECT ON local_ega.main_to_files TO ingest;
    GRANT USAGE, SELECT ON SEQUENCE local_ega.main_to_files_main_id_seq TO ingest;
    GRANT SELECT ON local_ega.files TO ingest;
    GRANT UPDATE ON local_ega.files TO ingest;
    GRANT UPDATE ON sda.files TO ingest;
    GRANT INSERT ON sda.checksums TO ingest;
    GRANT UPDATE ON sda.checksums TO ingest;
    GRANT SELECT ON sda.checksums TO ingest;
    GRANT USAGE, SELECT ON SEQUENCE sda.checksums_id_seq TO ingest;

    PERFORM create_role_if_not_exists('verify');
    GRANT USAGE ON SCHEMA sda TO verify;
    GRANT SELECT ON sda.files TO verify;
    GRANT UPDATE ON sda.files TO verify;
    GRANT INSERT ON sda.checksums TO verify;
    GRANT USAGE, SELECT ON SEQUENCE sda.checksums_id_seq TO verify;
    GRANT INSERT ON sda.file_event_log TO verify;
    GRANT USAGE, SELECT ON SEQUENCE sda.file_event_log_id_seq TO verify;
    GRANT USAGE ON SCHEMA local_ega TO verify;
    GRANT SELECT ON local_ega.files TO verify;
    GRANT UPDATE ON local_ega.files TO verify;
    GRANT SELECT ON local_ega.main_to_files TO verify;
    GRANT SELECT ON local_ega.status_translation TO verify;
    GRANT UPDATE ON local_ega.main TO verify;
    GRANT UPDATE ON sda.files TO verify;
    GRANT INSERT, SELECT, UPDATE ON sda.checksums TO verify;
    GRANT USAGE, SELECT ON SEQUENCE sda.checksums_id_seq TO verify;

    PERFORM create_role_if_not_exists('finalize');
    GRANT USAGE ON SCHEMA sda TO finalize;
    GRANT UPDATE ON sda.files TO finalize;
    GRANT SELECT ON sda.files TO finalize;
    GRANT SELECT ON sda.checksums TO finalize;
    GRANT INSERT ON sda.file_event_log TO finalize;
    GRANT USAGE, SELECT ON SEQUENCE sda.file_event_log_id_seq TO finalize;

    GRANT USAGE ON SCHEMA local_ega TO finalize;
    GRANT SELECT ON local_ega.main_to_files TO finalize;
    GRANT SELECT ON local_ega.status_translation TO finalize;
    GRANT UPDATE ON local_ega.files TO finalize;
    GRANT SELECT ON local_ega.files TO finalize;
    GRANT UPDATE ON sda.files TO finalize;
    GRANT INSERT, SELECT, UPDATE ON sda.checksums TO finalize;
    GRANT USAGE, SELECT ON SEQUENCE sda.checksums_id_seq TO finalize;

    PERFORM create_role_if_not_exists('mapper');
    GRANT USAGE ON SCHEMA sda TO mapper;
    GRANT INSERT ON sda.datasets TO mapper;
    GRANT SELECT ON sda.datasets TO mapper;
    GRANT USAGE, SELECT ON SEQUENCE sda.datasets_id_seq TO mapper;
    GRANT SELECT ON sda.files TO mapper;
    GRANT INSERT ON sda.file_dataset TO mapper;
    GRANT SELECT ON local_ega.main_to_files TO mapper;
    GRANT USAGE, SELECT ON SEQUENCE sda.file_dataset_id_seq TO mapper;
    GRANT USAGE ON SCHEMA local_ega TO mapper;
    GRANT USAGE ON SCHEMA local_ega_ebi TO mapper;

    GRANT SELECT ON local_ega.archive_files TO mapper;
    GRANT INSERT ON local_ega_ebi.filedataset TO mapper;

    PERFORM create_role_if_not_exists('sync');
    GRANT USAGE ON SCHEMA sda TO sync;
    GRANT SELECT ON sda.files TO sync;
    GRANT SELECT ON sda.file_event_log TO sync;
    GRANT SELECT ON sda.checksums TO sync;
    GRANT USAGE ON SCHEMA local_ega TO sync;
    GRANT SELECT ON local_ega.files TO sync;
    GRANT UPDATE ON local_ega.main TO sync;

    PERFORM create_role_if_not_exists('download');
    GRANT USAGE ON SCHEMA sda TO download;
    GRANT SELECT ON sda.files TO download;
    GRANT SELECT ON sda.file_dataset TO download;
    GRANT USAGE ON SCHEMA local_ega TO download;
    GRANT USAGE ON SCHEMA local_ega_ebi TO download;
    GRANT SELECT ON local_ega.files TO download;
    GRANT SELECT ON local_ega_ebi.file TO download;
    GRANT SELECT ON local_ega_ebi.file_dataset TO download;

    GRANT base, ingest, verify, finalize, sync TO lega_in;
    GRANT USAGE ON SCHEMA sda TO lega_in;

    GRANT base, mapper, download TO lega_out;

    -- Drop temporary user creation function
    DROP FUNCTION create_role_if_not_exists;

  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$


