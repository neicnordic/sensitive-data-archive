DO
$$
DECLARE
-- The version we know how to do migration from, at the end of a successful migration
-- we will no longer be at this version.
  sourcever INTEGER := 8;
  changes VARCHAR := 'Add dataset event log';
BEGIN
  IF (select max(version) from sda.dbschema_version) = sourcever then
    RAISE NOTICE 'Doing migration from schema version % to %', sourcever, sourcever+1;
    RAISE NOTICE 'Changes: %', changes;
    INSERT INTO sda.dbschema_version VALUES(sourcever+1, now(), changes);

    CREATE TABLE dataset_events (
        id          SERIAL PRIMARY KEY,
        title       VARCHAR(64) UNIQUE, -- short name of the action
        description TEXT
    );

    INSERT INTO dataset_events(id,title,description)
    VALUES (10, 'registered', 'A dataset has been registered'),
           (20, 'released'  , 'The dataset is released on this date'),
           (30, 'deprecated', 'The dataset is deprecated on this date');

    CREATE TABLE dataset_event_log (
        id         SERIAL PRIMARY KEY,
        dataset_id TEXT REFERENCES datasets(stable_id),
        event      TEXT REFERENCES dataset_events(title),
        message    JSONB, -- The rabbitMQ message that initiated the dataset event
        event_date TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT clock_timestamp()
    );
  ELSE
    RAISE NOTICE 'Schema migration from % to % does not apply now, skipping', sourcever, sourcever+1;
  END IF;
END
$$
