# Data Migration Plan POST schema migration version 23


## 1. Ensure schema migration has taken place
Ensure [23_expand_files_table_with_storage_locations.sql](../migratedb.d/23_expand_files_table_with_storage_locations.sql)
has been executed.

Can be checked by
```sql
SELECT * from sda.dbschema_version WHERE version = 23;
```

## 2. Prep
Note: Prep is only needed if you have multiple s3 buckets / posix volumes for a storage
   
Repeat steps for each s3 bucket / posix volume 

### 2.1. Get file ids file for a storage 
 
#### If S3 storage
Get all files in form each s3 bucket
```bash
aws s3api list-objects-v2 --endpoint ${ENDPOINT} --bucket ${BUCKET} > ${BUCKET}_raw
```

Transform raw response to just list of ids
```bash
jq -r '.Contents[].Key' ${BUCKET}_raw > ${BUCKET}_ids
```

#### If Posix storage
``` bash
find . -type f -exec basename {} \; > ${POSIX_VOLUME}_ids
```

### 2.2. Create new temporary tables to support DB migration

```sql
CREATE TABLE sda.temp_file_in_${BUCKET || POSIX_VOLUME} ( 
file_id UUID PRIMARY KEY
);
``` 

### 2.3. Populate tables
```bash
psql -U $user -d sda -At -h $host -p $port -c "\copy sda.temp_file_in_${BUCKET || POSIX_VOLUME} from '/path/to/${BUCKET || POSIX_VOLUME}_ids' with delimiter as ','"
```

## 3. Run data migration queries
Run the following data migrations in a transaction such that transaction can be aborted incase something goes wrong and rollback is desired.

So if something goes wrong after [3.1 Start transaction](#31-start-transaction) and before 
[3.5 Commit transaction](#35-commit) you can run
```sql
ROLLBACK;
```
to rollback the transaction.

### 3.1. Start transaction
```sql
BEGIN;
```

### 3.2. Inbox Location

If posix inbox replace `${INBOX_ENDPOINT}/${INBOX_BUCKET}` with `${INBOX_POSIX_VOLUME}`

If you only have one inbox storage
```sql
UPDATE sda.files
SET submission_location = '${INBOX_ENDPOINT}/${INBOX_BUCKET}';
```

If you only have multiple inbox storages, repeat following UPDATE statement per bucket/volume you have
```sql
UPDATE sda.files AS f
SET submission_location = '${INBOX_ENDPOINT}/${INBOX_BUCKET}'
FROM temp_file_in_${INBOX_BUCKET} AS in_buk
WHERE f.id = in_buk.file_id;
```

### 3.3. Archive Location

If posix archive replace `${ARCHIVE_ENDPOINT}/${ARCHIVE_BUCKET}` with `/${ARCHIVE_POSIX_VOLUME}`

If you only have one archive storage
```sql
UPDATE sda.files 
SET archive_location ='${ARCHIVE_ENDPOINT}/${ARCHIVE_BUCKET}'
WHERE archive_file_path != '';
```

If you only have multiple archive storages, repeat following UPDATE statement per bucket/volume you have
```sql
UPDATE sda.files AS f
SET archive_location = '${ARCHIVE_ENDPOINT}/${ARCHIVE_BUCKET}'
FROM temp_file_in_${ARCHIVE_BUCKET} AS in_buk 
WHERE f.id = in_buk.file_id;
```

### 3.4 Backup location
Skip this if you do not have a backup storage

If posix archive replace '${BACKUP_ENDPOINT}/${BACKUP_BUCKET}' with '/${BACKUP_POSIX_VOLUME}'

If you only have one backup storage
```sql
UPDATE sda.files 
SET backup_location ='${BACKUP_ENDPOINT}/${BACKUP_BUCKET}'
WHERE stable_id IS NOT NULL;
```
If you only have multiple backup storages, repeat following UPDATE statement per bucket/volume you have
```sql
UPDATE sda.files AS f
SET backup_location = '${BACKUP_ENDPOINT}/${BACKUP_BUCKET}'
FROM temp_file_in_${BACKUP_BUCKET} AS in_buk 
WHERE f.id = in_buk.file_id;
```

### 3.5 Commit
Commit the transaction
```sql
COMMIT;
```

## 4. Clean up
Only needed if you did the [2. Prep step](#2-prep) and created temporary tables

Repeat DROP table statement per temporary table created
```sql
DROP TABLE sda.temp_file_in_${BUCKET || POSIX_VOLUME}; 
```

## 5. Ensure all files have been updated
```sql
SELECT count(id) FROM sda.files WHERE submission_location IS NULL OR (archive_location IS NULL AND archive_file_path != '')
```
If there exists rows, then there are issues and the required locations of the files are not known.
To resolve you could either manually delete those sda.files entries or ensure the files are uploaded to the expected locations.

### 5.1 Backup location verification
Skip this step if you do not have a backup storage

```sql
SELECT count(id) FROM sda.files WHERE archive_location IS NULL AND stable_id IS NOT NULL)
```
If there exists rows, then there are issues and the backup locations of the files are not known.
To resolve you could either manually delete those sda.files entries or ensure the files are uploaded to the expected locations.
