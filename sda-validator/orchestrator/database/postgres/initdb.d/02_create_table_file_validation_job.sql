CREATE TABLE file_validation_job
(
    id                   SERIAL PRIMARY KEY,
    validation_id        UUID,
    validator_id         TEXT,
    file_path            TEXT,
    file_id              UUID,
    submission_file_size BIGINT,
    submission_user      TEXT,
    triggered_by         TEXT,
    file_result          TEXT                              DEFAULT 'pending',
    started_at           TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT clock_timestamp(),
    finished_at          TIMESTAMP,
    file_messages        JSON,
    validator_messages   JSON,
    validator_result     TEXT                              DEFAULT 'pending',

    CONSTRAINT unique_file_validation_job UNIQUE (validation_id, validator_id, file_id)
);