#!/bin/sh
set -eou pipefail
fileID="33d29907-c565-4a90-98b4-e31b992ab376"
export PGPASSWORD=inbox

for host in migrate postgres; do
    fileID=$(psql -U inbox -h "$host" -d sda -At -c "SELECT sda.register_file('$fileID', 'inbox/test-file.c4gh', 'test-user');")
    if [ -z "$fileID" ]; then
        echo "register_file failed"
        exit 1
    fi

    newFileID=$(psql -U inbox -h "$host" -d sda -At -c "SELECT sda.register_file(null, 'inbox/test-file.c4gh', 'other-user');")
    if [ -z "$newFileID" ]; then
        echo "register_file failed"
        exit 1
    fi

    if [ "$fileID" = "$newFileID" ]; then
        echo "File IDs should not be the same"
        exit 1
    fi

    resp=$(psql -U inbox -h "$host" -d sda -At -c "INSERT INTO sda.file_event_log(file_id, event, user_id, message) VALUES('$fileID', 'uploaded', 'test-user', '{\"uploaded\": \"message\"}');")
    if [ "$resp" != "INSERT 0 1" ]; then
        echo "Mark file as uploaded failed"
        exit 1
    fi
done

echo "20_inbox_queries completed successfully"
