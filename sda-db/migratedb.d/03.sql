
DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 2;
  changes VARCHAR := 'Reorganized out views';
BEGIN
-- No explicit transaction handling here, this all happens in a transaction 
-- automatically 
  IF (select max(version) from local_ega.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO local_ega.dbschema_version VALUES(sourcever+1, now(), changes);
    ALTER TABLE local_ega_ebi.filedataset ADD UNIQUE(file_id, dataset_stable_id);

    DROP VIEW IF EXISTS local_ega_ebi.file;
    CREATE OR REPLACE VIEW local_ega_ebi.file AS
    SELECT stable_id                                AS file_id,
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
           decrypted_file_size			    AS decrypted_file_size,
           decrypted_file_checksum		    AS decrypted_file_checksum,
           decrypted_file_checksum_type		    AS decrypted_file_checksum_type,
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


