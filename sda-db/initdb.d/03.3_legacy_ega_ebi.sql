CREATE SCHEMA local_ega_ebi;

SET search_path TO local_ega_ebi;

-- Special view for EBI Data-Out
CREATE VIEW local_ega_ebi.file AS
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
       decrypted_file_size                      AS decrypted_file_size,
       decrypted_file_checksum                  AS decrypted_file_checksum,
       decrypted_file_checksum_type             AS decrypted_file_checksum_type,
       status                                   AS file_status,
       header                                   AS header
FROM local_ega.main
WHERE status = 'READY';

-- Relation File EGAF <-> Dataset EGAD
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
                    (SELECT id FROM sda.datasets WHERE stable_id = NEW.dataset_stable_id))
            ON CONFLICT DO NOTHING;

        RETURN NEW;
    END;
$filedataset_insert$ LANGUAGE plpgsql;

CREATE TRIGGER filedataset_insert_trigger
    INSTEAD OF INSERT ON local_ega_ebi.filedataset
    FOR EACH ROW EXECUTE PROCEDURE filedataset_insert();

-- This view was created to be in sync with the entity eu.elixir.ega.ebi.downloader.domain.entity.FileDataset
-- which uses a view and has an @Id annotation in file_id
CREATE VIEW local_ega_ebi.file_dataset AS
SELECT m.stable_id AS file_id, dataset_stable_id as dataset_id FROM local_ega_ebi.filedataset fd
INNER JOIN local_ega.main m ON fd.file_id=m.id;

-- Relation File <-> Index File
CREATE TABLE local_ega_ebi.fileindexfile (
       id       SERIAL, PRIMARY KEY(id), UNIQUE (id),
       file_id     INTEGER NOT NULL REFERENCES local_ega.main_to_files (main_id) ON DELETE CASCADE, -- not stable_id
       index_file_id TEXT,
       index_file_reference      TEXT NOT NULL,     -- file path if POSIX, object id if S3
       index_file_type           local_ega.storage  -- S3 or POSIX file system
);

-- This view was created to be in sync with the entity eu.elixir.ega.ebi.downloader.domain.entity.FileIndexFile
-- which seems to use a view and has an @Id annotation in file_id
CREATE VIEW local_ega_ebi.file_index_file AS
SELECT m.stable_id AS file_id, index_file_id FROM local_ega_ebi.fileindexfile fif
INNER JOIN local_ega.main m ON fif.file_id=m.id;
