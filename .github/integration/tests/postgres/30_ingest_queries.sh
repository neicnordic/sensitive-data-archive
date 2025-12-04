#!/bin/sh
set -eou pipefail

export PGPASSWORD=ingest
user="test-user"
fileID="33d29907-c565-4a90-98b4-e31b992ab376"

for host in migrate postgres; do
    ## insert file
    fileID=$(psql -U ingest -h "$host" -d sda -At -c "SELECT sda.register_file('$fileID', 'inbox/test-file.c4gh', '$user');")
    if [ -z "$fileID" ]; then
        echo "register_file failed"
        exit 1
    fi

    resp=$(psql -U ingest -h "$host" -d sda -At -c "INSERT INTO sda.file_event_log(file_id, event, user_id, message) VALUES('$fileID', 'submitted', '$user', '{}');")
    if [ "$(echo "$resp" | tr -d '\n')" != "INSERT 0 1" ]; then
        echo "insert file failed"
        exit 1
    fi

    ## store header
    resp=$(psql -U ingest -h "$host" -d sda -At -c "UPDATE sda.files SET header = '637279707434676801000000010000006c00000000000000' WHERE id = '$fileID';")
    if [ "$resp" != "UPDATE 1" ]; then
        echo "store header failed"
        exit 1
    fi

    ## set archived
    archive_path=d853c51b-6aed-4243-b427-177f5e588857
    size="2035150"
    checksum="f03775a50feea74c579d459fdbeb27adafd543b87f6692703543a6ebe7daa1ff"

    resp=$(psql -U ingest -h "$host" -d sda -At -c "UPDATE sda.files SET archive_file_path = '$archive_path', archive_file_size = '$size' WHERE id = '$fileID';")
    if [ "$resp" != "UPDATE 1" ]; then
        echo "update of files.archive_file_path, archive_file_size failed: $resp"
        exit 1
    fi
    resp=$(psql -U ingest -h "$host" -d sda -At -c "INSERT INTO sda.checksums(file_id, checksum, type, source) VALUES('$fileID', '$checksum', upper('SHA256')::sda.checksum_algorithm, upper('UPLOADED')::sda.checksum_source);")
    if [ "$(echo "$resp" | tr -d '\n')" != "INSERT 0 1" ]; then
        echo "insert of archived checksum failed: $resp"
        exit 1
    fi
    resp=$(psql -U ingest -h "$host" -d sda -At -c "INSERT INTO sda.file_event_log(file_id, event) VALUES('$fileID', 'archived');")
    if [ "$(echo "$resp" | tr -d '\n')" != "INSERT 0 1" ]; then
        echo "insert of file_event_log failed: $resp"
        exit 1
    fi

done

echo "30_ingest_queries completed successfully"
