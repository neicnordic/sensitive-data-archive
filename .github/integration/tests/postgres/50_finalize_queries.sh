#!/bin/sh
set -eou pipefail

export PGPASSWORD=finalize

for host in migrate postgres; do
    ## mark file as 'READY'
    accession="urn:uuid:7964e232-8830-4351-8adb-e4ebb71fafed"
    user="test-user"
    inbox_path="inbox/test-file.c4gh"
    decrypted_checksum="febee6829a05772eea93c647e38bf5cc5bf33d1bcd0ea7d7bdd03225d84d2553"
    resp=$(psql -U finalize -h "$host" -d sda -At -c "UPDATE local_ega.files SET status = 'READY', stable_id = '$accession' WHERE elixir_id = '$user' and inbox_path = '$inbox_path' and decrypted_file_checksum = '$decrypted_checksum' and status = 'COMPLETED';")
    if [ "$resp" != "UPDATE 1" ]; then
        echo "mark file ready failed"
        exit 1
    fi
done
