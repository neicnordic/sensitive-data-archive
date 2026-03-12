# Schema migration rollback version 23
The following instructions describe the procedure to rollback schema version 23.

## Ensure current schema version
Ensure current schema version is at: 23

```sql
SELECT max(version) AS current_version FROM sda.dbschema_version;
```
if result of query is not 23, do not proceed with instructions.

## Rollback instructions
The schema rollback is recommended to be executed in a transaction, as if something goes wrong during the rollback
it can be aborted by rolling back transaction with the following statement
```sql
ROLLBACK;
```

### Start transaction
```sql
BEGIN;
```
### Do schema rollback

```sql
DROP FUNCTION IF EXISTS sda.register_file;
CREATE FUNCTION sda.register_file(file_id TEXT, submission_file_path TEXT, submission_user TEXT)
    RETURNS TEXT AS $register_file$
DECLARE
file_uuid UUID;
BEGIN
        -- Upsert file information. we're not interested in restarted uploads so old
        -- overwritten files that haven't been ingested are updated instead of
        -- inserting a new row.
INSERT INTO sda.files( id, submission_file_path, submission_user, encryption_method )
VALUES(  COALESCE(CAST(NULLIF(file_id, '') AS UUID), gen_random_uuid()), submission_file_path, submission_user, 'CRYPT4GH' )
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

ALTER TABLE sda.files
    DROP COLUMN archive_location,
    DROP COLUMN backup_location,
    DROP COLUMN submission_location;

DELETE FROM sda.dbschema_version WHERE version = 23;         
```

### Commit transaction
```sql
COMMIT;
```