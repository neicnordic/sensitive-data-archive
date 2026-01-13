#!/bin/sh
set -eou pipefail

export PGPASSWORD=verify
fileID="33d29907-c565-4a90-98b4-e31b992ab376"

for host in migrate postgres; do

    ## get file status
    status=$(psql -U verify -h "$host" -d sda -At -c "SELECT event from sda.file_event_log WHERE file_id = '$fileID' ORDER BY id DESC LIMIT 1;")
    if [ "$status" = "" ]; then
        echo "get file status failed: $resp"
        exit 1
    fi

    ## get file header
    header="637279707434676801000000010000006c00000000000000"
    dbheader=$(psql -U verify -h "$host" -d sda -At -c "SELECT header from sda.files WHERE id = '$fileID';")
    if [ "$dbheader" != "$header" ]; then
        echo "wrong header received: $resp"
        exit 1
    fi

    ## mark file as 'COMPLETED'
    archive_checksum="64e56b0d245b819c116b5f1ad296632019490b57eeaebb419a5317e24a153852"
    decrypted_size="2034254"
    decrypted_checksum="febee6829a05772eea93c647e38bf5cc5bf33d1bcd0ea7d7bdd03225d84d2553"

    resp=$(psql -U verify -h "$host" -d sda -At -c "UPDATE sda.files SET decrypted_file_size = '$decrypted_size' WHERE id = '$fileID';")
    if [ "$resp" != "UPDATE 1" ]; then
        echo "update of files.decrypted_file_size failed: $resp"
        exit 1
    fi

    resp=$(psql -U verify -h "$host" -d sda -At -c "INSERT INTO sda.checksums(file_id, checksum, type, source) VALUES('$fileID', '$archive_checksum', upper('SHA256')::sda.checksum_algorithm, upper('ARCHIVED')::sda.checksum_source);")
    if [ "$(echo "$resp" | tr -d '\n')" != "INSERT 0 1" ]; then
        echo "insert of archived checksum failed: $resp"
        exit 1
    fi

    resp=$(psql -U verify -h "$host" -d sda -At -c "INSERT INTO sda.checksums(file_id, checksum, type, source) VALUES('$fileID', '$decrypted_checksum', upper('SHA256')::sda.checksum_algorithm, upper('UNENCRYPTED')::sda.checksum_source);")
    if [ "$(echo "$resp" | tr -d '\n')" != "INSERT 0 1" ]; then
        echo "insert of decrypted checksum failed: $resp"
        exit 1
    fi

    resp=$(psql -U verify -h "$host" -d sda -At -c "INSERT INTO sda.file_event_log(file_id, event) VALUES('$fileID', 'verified');")
    if [ "$(echo "$resp" | tr -d '\n')" != "INSERT 0 1" ]; then
        echo "insert of file_event_log failed: $resp"
        exit 1
    fi


done

echo "40_verify_queries completed successfully"
