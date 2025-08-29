-- \connect lega

-- users

CREATE USER lega_in;
CREATE USER lega_out;

-- Roles are created with minimal permissions for each pipeline service, then
-- these roles are granted to database users. Roles are granted based on the
-- functions that are used in the sda-pipeline database interface.
-- Permissions have been laxed slightly by generally allowing SELECT and INSERT
-- on all fields, while UPDATES are limited as they are more destructive.

CREATE ROLE base;
GRANT USAGE ON SCHEMA sda TO base;
GRANT USAGE ON SCHEMA local_ega TO base;
GRANT SELECT ON sda.dbschema_version TO base;
GRANT SELECT ON local_ega.dbschema_version TO base;

CREATE ROLE inbox;
-- uses: db.InsertFile
GRANT USAGE ON SCHEMA sda TO inbox;
GRANT SELECT, INSERT, UPDATE ON sda.files TO inbox;
GRANT SELECT, INSERT ON sda.file_event_log TO inbox;
GRANT USAGE, SELECT ON SEQUENCE sda.file_event_log_id_seq TO inbox;

-- legacy schema
GRANT USAGE ON SCHEMA local_ega TO inbox;
GRANT INSERT, SELECT ON local_ega.main_to_files TO inbox;
GRANT USAGE, SELECT ON SEQUENCE local_ega.main_to_files_main_id_seq TO inbox;

CREATE ROLE ingest;
-- uses: db.InsertFile, db.StoreHeader, and db.SetArchived
GRANT USAGE ON SCHEMA sda TO ingest;
GRANT INSERT ON sda.files TO ingest;
GRANT SELECT ON sda.files TO ingest;
GRANT UPDATE ON sda.files TO ingest;
GRANT INSERT ON sda.checksums TO ingest;
GRANT UPDATE ON sda.checksums TO ingest;
GRANT SELECT ON sda.checksums TO ingest;
GRANT INSERT ON sda.file_events TO ingest;
GRANT USAGE, SELECT ON SEQUENCE sda.checksums_id_seq TO ingest;
GRANT INSERT ON sda.file_event_log TO ingest;
GRANT SELECT ON sda.file_event_log TO ingest;
GRANT USAGE, SELECT ON SEQUENCE sda.file_event_log_id_seq TO ingest;
GRANT SELECT ON sda.encryption_keys TO ingest;
GRANT INSERT ON sda.encryption_keys TO ingest;

-- legacy schema
GRANT USAGE ON SCHEMA local_ega TO ingest;
GRANT INSERT, SELECT ON local_ega.main TO ingest;
GRANT SELECT ON local_ega.status_translation TO ingest;
GRANT INSERT, SELECT ON local_ega.main_to_files TO ingest;
GRANT USAGE, SELECT ON SEQUENCE local_ega.main_to_files_main_id_seq TO ingest;
GRANT SELECT ON local_ega.files TO ingest;
GRANT UPDATE ON local_ega.files TO ingest;
GRANT UPDATE ON local_ega.main TO ingest;

--------------------------------------------------------------------------------

CREATE ROLE verify;
-- uses: db.GetHeader, and db.MarkCompleted
GRANT USAGE ON SCHEMA sda TO verify;
GRANT SELECT ON sda.files TO verify;
GRANT UPDATE ON sda.files TO verify;
GRANT INSERT ON sda.checksums TO verify;
GRANT USAGE, SELECT ON SEQUENCE sda.checksums_id_seq TO verify;
GRANT INSERT ON sda.file_event_log TO verify;
GRANT SELECT ON sda.file_event_log TO verify;
GRANT USAGE, SELECT ON SEQUENCE sda.file_event_log_id_seq TO verify;

-- legacy schema
GRANT USAGE ON SCHEMA local_ega TO verify;
GRANT SELECT ON local_ega.files TO verify;
GRANT UPDATE ON local_ega.files TO verify;
GRANT SELECT ON local_ega.main_to_files TO verify;
GRANT SELECT ON local_ega.status_translation TO verify;
GRANT UPDATE ON local_ega.main TO verify;
GRANT INSERT, SELECT, UPDATE ON sda.checksums TO verify;
GRANT USAGE, SELECT ON SEQUENCE sda.checksums_id_seq TO verify;

--------------------------------------------------------------------------------

CREATE ROLE finalize;
-- uses: db.MarkReady
GRANT USAGE ON SCHEMA sda TO finalize;
GRANT UPDATE ON sda.files TO finalize;
GRANT SELECT ON sda.files TO finalize;
GRANT SELECT ON sda.checksums TO finalize;
GRANT INSERT ON sda.file_event_log TO finalize;
GRANT SELECT ON sda.file_event_log TO finalize;
GRANT USAGE, SELECT ON SEQUENCE sda.file_event_log_id_seq TO finalize;

-- legacy schema
GRANT USAGE ON SCHEMA local_ega TO finalize;
GRANT SELECT ON local_ega.main_to_files TO finalize;
GRANT SELECT ON local_ega.status_translation TO finalize;
GRANT UPDATE ON local_ega.files TO finalize;
GRANT SELECT ON local_ega.files TO finalize;
GRANT INSERT, SELECT, UPDATE ON sda.checksums TO finalize;
GRANT USAGE, SELECT ON SEQUENCE sda.checksums_id_seq TO finalize;

--------------------------------------------------------------------------------

CREATE ROLE mapper;
-- uses: db.MapFilesToDataset
GRANT USAGE ON SCHEMA sda TO mapper;
GRANT INSERT ON sda.datasets TO mapper;
GRANT SELECT ON sda.datasets TO mapper;
GRANT USAGE, SELECT ON SEQUENCE sda.datasets_id_seq TO mapper;
GRANT SELECT ON sda.files TO mapper;
GRANT INSERT ON sda.file_event_log TO mapper;
GRANT INSERT ON sda.file_dataset TO mapper;
GRANT INSERT ON sda.dataset_event_log TO mapper;
GRANT USAGE, SELECT ON SEQUENCE sda.file_dataset_id_seq TO mapper;
GRANT USAGE, SELECT ON SEQUENCE sda.file_event_log_id_seq TO mapper;
GRANT USAGE, SELECT ON SEQUENCE sda.dataset_event_log_id_seq TO mapper;

-- legacy schema
GRANT USAGE ON SCHEMA local_ega TO mapper;
GRANT USAGE ON SCHEMA local_ega_ebi TO mapper;
GRANT SELECT ON local_ega.main_to_files TO mapper;
GRANT SELECT ON local_ega.archive_files TO mapper;
GRANT INSERT ON local_ega_ebi.filedataset TO mapper;
GRANT UPDATE ON local_ega.files TO mapper;

--------------------------------------------------------------------------------

CREATE ROLE sync;
-- uses: db.GetArchived
GRANT USAGE ON SCHEMA sda TO sync;
GRANT SELECT ON sda.files TO sync;
GRANT SELECT ON sda.file_event_log TO sync;
GRANT SELECT ON sda.checksums TO sync;

-- legacy schema
GRANT USAGE ON SCHEMA local_ega TO sync;
GRANT SELECT ON local_ega.files TO sync;
GRANT UPDATE ON local_ega.main TO sync;

--------------------------------------------------------------------------------


CREATE ROLE download;

GRANT USAGE ON SCHEMA sda TO download;
GRANT SELECT ON sda.files TO download;
GRANT SELECT ON sda.file_dataset TO download;
GRANT SELECT ON sda.checksums TO download;
GRANT SELECT ON sda.datasets TO download;
GRANT SELECT ON sda.file_event_log TO download;
GRANT SELECT ON sda.dataset_event_log TO download;

-- legacy schema
GRANT USAGE ON SCHEMA local_ega TO download;
GRANT USAGE ON SCHEMA local_ega_ebi TO download;
GRANT SELECT ON local_ega.files TO download;
GRANT SELECT ON local_ega_ebi.file TO download;
GRANT SELECT ON local_ega_ebi.file_dataset TO download;

--------------------------------------------------------------------------------

CREATE ROLE api;

GRANT USAGE ON SCHEMA sda TO api;
GRANT SELECT ON sda.files TO api;
GRANT SELECT ON sda.file_dataset TO api;
GRANT SELECT ON sda.checksums TO api;
GRANT SELECT, INSERT ON sda.file_event_log TO api;
GRANT SELECT ON sda.encryption_keys TO api;
GRANT SELECT ON sda.datasets TO api;
GRANT SELECT ON sda.dataset_event_log TO api;
GRANT INSERT ON sda.encryption_keys TO api;
GRANT UPDATE ON sda.encryption_keys TO api;
GRANT USAGE, SELECT ON SEQUENCE sda.file_event_log_id_seq TO api;

-- legacy schema
GRANT USAGE ON SCHEMA local_ega TO api;
GRANT USAGE ON SCHEMA local_ega_ebi TO api;
GRANT SELECT ON local_ega.files TO api;
GRANT SELECT ON local_ega_ebi.file TO api;
GRANT SELECT ON local_ega_ebi.file_dataset TO api;

--------------------------------------------------------------------------------
CREATE ROLE auth;
GRANT USAGE ON SCHEMA sda TO auth;
GRANT SELECT, INSERT, UPDATE ON sda.userinfo TO auth;
--------------------------------------------------------------------------------

-- lega_in permissions
GRANT base, ingest, verify, finalize, sync, api TO lega_in;

-- lega_out permissions
GRANT mapper, download, api TO lega_out;

GRANT base TO api, download, inbox, ingest, finalize, mapper, verify, auth;
