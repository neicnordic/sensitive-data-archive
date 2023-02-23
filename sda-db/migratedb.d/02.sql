
DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 1;
  changes VARCHAR := 'Add columns for decrypted checksum ';
BEGIN
-- No explicit transaction handling here, this all happens in a transaction 
-- automatically 
  IF (select max(version) from local_ega.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO local_ega.dbschema_version VALUES(sourcever+1, now(), changes);
    ALTER TABLE local_ega.main ADD COLUMN IF NOT EXISTS decrypted_file_checksum VARCHAR(128);
    ALTER TABLE local_ega.main ADD COLUMN IF NOT EXISTS decrypted_file_checksum_type local_ega.checksum_algorithm;
    ALTER TABLE local_ega.main ADD COLUMN IF NOT EXISTS decrypted_file_size BIGINT;
    DROP VIEW IF EXISTS local_ega.files;
    CREATE OR REPLACE VIEW local_ega.files AS
    SELECT id,
       submission_user                          AS elixir_id,
       submission_file_path                     AS inbox_path,
       submission_file_size                     AS inbox_filesize,
       submission_file_calculated_checksum      AS inbox_file_checksum,
       submission_file_calculated_checksum_type AS inbox_file_checksum_type,
       status,
       archive_file_reference                     AS archive_path,
       archive_file_type                          AS archive_type,
       archive_file_size                          AS archive_filesize,
       archive_file_checksum                      AS archive_file_checksum,
       archive_file_checksum_type                 AS archive_file_checksum_type,
       decrypted_file_size			  AS decrypted_file_size,
       decrypted_file_checksum			  AS decrypted_file_checksum,
       decrypted_file_checksum_type		  AS decrypted_file_checksum_type,
       stable_id,
       header,  -- Crypt4gh specific
       version,
       created_at,
       last_modified
     FROM local_ega.main;

     DROP VIEW IF EXISTS local_ega_ebi.file;
     CREATE OR REPLACE VIEW local_ega_ebi.file AS
     SELECT stable_id                                AS file_id,
            archive_file_reference                   AS file_name,
            archive_file_reference                   AS file_path,
            reverse(split_part(reverse(submission_file_path::text), '/'::text, 1)) AS display_file_name,
            archive_file_size                        AS file_size,
            NULL::text                               AS checksum,
            NULL::text                               AS checksum_type,
            archive_file_checksum                    AS unencrypted_checksum,
            archive_file_checksum_type               AS unencrypted_checksum_type,
            decrypted_file_size                      AS decrypted_file_size,
            decrypted_file_checksum                  AS decrypted_file_checksum,
            decrypted_file_checksum_type             AS decrypted_file_checksum_type,
            status                                   AS file_status,
            header                                   AS header
     FROM local_ega.main
     WHERE status = 'READY';

     GRANT USAGE ON SCHEMA local_ega TO lega_in, lega_out;
     GRANT ALL PRIVILEGES ON ALL TABLES    IN SCHEMA local_ega TO lega_in; -- Read/Write access on local_ega.* for lega_in
     GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA local_ega TO lega_in; -- Don't forget the sequences
     GRANT SELECT ON local_ega.archive_files  TO lega_out;                    -- Read-Only access for lega_out

     -- Set up rights access for audit schema
     GRANT USAGE ON SCHEMA local_ega_download TO lega_out;
     GRANT ALL PRIVILEGES ON ALL TABLES    IN SCHEMA local_ega_download TO lega_out; -- Read/Write on audit.* for lega_out
     GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA local_ega_download TO lega_out; -- Don't forget the sequences

     -- Set up rights access for local_ega_ebi schema
     GRANT USAGE ON SCHEMA local_ega_ebi TO lega_out;
     GRANT ALL PRIVILEGES ON ALL TABLES    IN SCHEMA local_ega_ebi TO lega_out;
     GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA local_ega_ebi TO lega_out;

  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$


