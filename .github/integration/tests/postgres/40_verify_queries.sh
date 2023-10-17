#!/bin/sh
set -eou pipefail

export PGPASSWORD=verify
corrID="33d29907-c565-4a90-98b4-e31b992ab376"

for host in migrate postgres; do
    fileID=$(psql -U verify -h "$host" -d sda -At -c "SELECT DISTINCT file_id from sda.file_event_log WHERE correlation_id = '$corrID';")

    ## get file status
    status=$(psql -U verify -h "$host" -d sda -At -c "SELECT event from sda.file_event_log WHERE correlation_id = '$corrID' ORDER BY id DESC LIMIT 1;")
    if [ "$status" = "" ]; then
        echo "get file status failed"
        exit 1
    fi

    ## get file header
    header="637279707434676801000000010000006c00000000000000"
    dbheader=$(psql -U verify -h "$host" -d sda -At -c "SELECT header from sda.files WHERE id = '$fileID';")
    if [ "$dbheader" != "$header" ]; then
        echo "wrong header recieved"
        exit 1
    fi

    ## mark file as 'COMPLETED'
    archive_checksum="64e56b0d245b819c116b5f1ad296632019490b57eeaebb419a5317e24a153852"
    decrypted_size="2034254"
    decrypted_checksum="febee6829a05772eea93c647e38bf5cc5bf33d1bcd0ea7d7bdd03225d84d2553"
    resp=$(psql -U verify -h "$host" -d sda -At -c "SELECT sda.set_verified('$fileID', '$corrID', '$archive_checksum', 'SHA256', '$decrypted_size', '$decrypted_checksum', 'SHA256')")
    if [ "$resp" != "" ]; then
        echo "set_verified failed"
        exit 1
    fi
done
