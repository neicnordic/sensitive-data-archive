CREATE SCHEMA sda;

SET search_path TO sda;

-- ENUMS
CREATE TYPE checksum_algorithm AS ENUM ('MD5', 'SHA256', 'SHA384', 'SHA512');
CREATE TYPE checksum_source AS ENUM ('UPLOADED', 'ARCHIVED', 'UNENCRYPTED');

-- The schema_version table is used to keep track of migrations
CREATE TABLE  dbschema_version (
       version          INTEGER PRIMARY KEY,
       applied          TIMESTAMP WITH TIME ZONE,
       description      VARCHAR(1024)
);

INSERT INTO dbschema_version
VALUES (0, now(), 'Created with version'),
       (1, now(), 'Noop version'),
       (2, now(), 'Added decrypted_checksum et al'),
       (3, now(), 'Reorganized out views/tables'),
       (4, now(), 'Refactored schema'),
       (5, now(), 'Add field for correlation ids'),
       (6, now(), 'Add created_at field to datasets'),
       (7, now(), 'Add permissions to mapper to files'),
       (8, now(), 'Add ingestion functions'),
       (9, now(), 'Add dataset event log'),
       (10, now(), 'Create Inbox user');

-- Datasets are used to group files, and permissions are set on the dataset
-- level
CREATE TABLE datasets (
    id                  SERIAL PRIMARY KEY,
    stable_id           TEXT UNIQUE,
    title               TEXT,
    description         TEXT,
    created_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT clock_timestamp()
);

-- `files` is the main table of the schema, holding the file paths, encryption
-- header, and stable id.
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

    -- Table Audit / Logs
    created_by           NAME DEFAULT CURRENT_USER, -- Postgres users
    last_modified_by     NAME DEFAULT CURRENT_USER, --
    created_at           TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT clock_timestamp(),
    last_modified        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT unique_ingested UNIQUE(submission_file_path, archive_file_path)
);

-- To allow for multiple checksums per file, we use a dedicated table for it
CREATE TABLE checksums (
    id                  SERIAL PRIMARY KEY,
    file_id             UUID REFERENCES files(id),
    checksum            TEXT,
    type                checksum_algorithm,
    source              checksum_source,
    CONSTRAINT unique_checksum UNIQUE(file_id, type, source)
);

-- Dataset and references are identifiers used to access and reference the
-- dataset, such as DOIs. There can be multiple identifiers for each file or
-- dataset, and these may change over time.
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

-- connects files to datasets
CREATE TABLE file_dataset (
    id                  SERIAL PRIMARY KEY,
    file_id             UUID REFERENCES files(id) NOT NULL,
    dataset_id          INT REFERENCES datasets(id) NOT NULL,
    CONSTRAINT unique_file_dataset UNIQUE(file_id, dataset_id)
);

-- This table is used to define events for file event logging.
CREATE TABLE file_events (
    id                  SERIAL PRIMARY KEY,
    title               VARCHAR(64) UNIQUE, -- short name of the action
    description         TEXT
);

-- These are the default file events to log.
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


-- Keeps track of all events for the files, with timestamps and user_ids.
CREATE TABLE file_event_log (
    id                  SERIAL PRIMARY KEY,
    file_id             UUID REFERENCES files(id),
    event               TEXT REFERENCES file_events(title),
    correlation_id      UUID, -- Correlation ID in the message's header
    user_id             TEXT, -- Elixir user id (or pipeline-step for ingestion,
                              -- etc.)
    details             JSONB,  -- This is my solution to fields such as
                                -- download.requests.client_ip,
                                -- download.success.bytes, etc. is it any good?
                                -- Well, it's simpler, but harder to see what
                                -- data is available... What do we prefer?
    message             JSONB, -- The rabbitMQ message that initiated the file event
    started_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT clock_timestamp(),
    finished_at         TIMESTAMP,
    success             BOOLEAN,
    error               TEXT
);

-- This table is used to define events for dataset event logging.
CREATE TABLE dataset_events (
    id          SERIAL PRIMARY KEY,
    title       VARCHAR(64) UNIQUE, -- short name of the action
    description TEXT
);

-- These are the default dataset events to log.
INSERT INTO dataset_events(id,title,description)
VALUES (10, 'registered', 'Register a dataset to receive file accession IDs mappings.'),
       (20, 'released'  , 'The dataset is released on this date'),
       (30, 'deprecated', 'The dataset is deprecated on this date');


-- Keeps track of all events for the datasets, with timestamps.
CREATE TABLE dataset_event_log (
    id         SERIAL PRIMARY KEY,
    dataset_id TEXT REFERENCES datasets(stable_id),
    event      TEXT REFERENCES dataset_events(title),
    message    JSONB, -- The rabbitMQ message that initiated the dataset event
    event_date TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT clock_timestamp()
);
