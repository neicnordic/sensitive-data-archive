DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 8;
  changes VARCHAR := 'Add dataset event log';
BEGIN
  SET search_path TO sda;
  IF (select max(version) from sda.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO sda.dbschema_version VALUES(sourcever+1, now(), changes);

    CREATE TABLE sda.dataset_events (
        id          SERIAL PRIMARY KEY,
        title       VARCHAR(64) UNIQUE, -- short name of the action
        description TEXT
    );

    INSERT INTO sda.dataset_events(id,title,description)
    VALUES (10, 'registered', 'Register a dataset to receive file accession IDs mappings.'),
           (20, 'released'  , 'The dataset is released on this date'),
           (30, 'deprecated', 'The dataset is deprecated on this date');

    CREATE TABLE sda.dataset_event_log (
        id         SERIAL PRIMARY KEY,
        dataset_id TEXT REFERENCES datasets(stable_id),
        event      TEXT REFERENCES dataset_events(title),
        message    JSONB, -- The rabbitMQ message that initiated the dataset event
        event_date TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT clock_timestamp()
    );

    -- add new permissions
    GRANT INSERT ON sda.dataset_event_log TO mapper;
    GRANT USAGE, SELECT ON SEQUENCE sda.dataset_event_log_id_seq TO mapper;
  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$
