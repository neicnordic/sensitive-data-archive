#!/bin/sh
set -eou pipefail

export PGPASSWORD=ingest
user="test-user"
corrID="33d29907-c565-4a90-98b4-e31b992ab376"

## insert file
fileID=$(psql -U ingest -h postgres -d sda -At -c "SELECT sda.register_file('inbox/test-file.c4gh', '$user');")
if [ -z "$fileID" ]; then
    echo "register_file failed"
    exit 1
fi

resp=$(psql -U ingest -h postgres -d sda -At -c "INSERT INTO sda.file_event_log(file_id, event, correlation_id, user_id, message) VALUES('$fileID', 'submitted', '$corrID', '$user', '{}');")
if [ "$(echo "$resp" | tr -d '\n')" != "INSERT 0 1" ]; then
    echo "insert file failed"
    exit 1
fi

## store header
resp=$(psql -U ingest -h postgres -d sda -At -c "UPDATE sda.files SET header = '637279707434676801000000010000006c00000000000000' WHERE id = '$fileID';")
if [ "$resp" != "UPDATE 1" ]; then
    echo "store header failed"
    exit 1
fi

## set archived
archive_path=d853c51b-6aed-4243-b427-177f5e588857
size="2035150"
checksum="f03775a50feea74c579d459fdbeb27adafd543b87f6692703543a6ebe7daa1ff"
resp=$(psql -U ingest -h postgres -d sda -At -c "SELECT sda.set_archived('$fileID', '$corrID', '$archive_path', '$size', '$checksum', 'SHA256');")
if [ "$resp" != "" ]; then
    echo "mark file archived failed"
    exit 1
fi
