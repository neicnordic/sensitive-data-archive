#!/bin/sh
set -eou pipefail

export PGPASSWORD=verify

## get file header
header="637279707434676801000000010000006c00000000000000"
dbheader=$(psql -U verify -h postgres -d sda -At -c "SELECT header from local_ega.files WHERE id = 1;")
if [ "$dbheader" != "$header" ]; then
    echo "wrong header recieved"
    exit 1
fi

## mark file as 'COMPLETED'
archive_checksum="64e56b0d245b819c116b5f1ad296632019490b57eeaebb419a5317e24a153852"
archive_size="2035150"
decrypted_size="2034254"
decrypted_checksum="febee6829a05772eea93c647e38bf5cc5bf33d1bcd0ea7d7bdd03225d84d2553"
resp=$(psql -U verify -h postgres -d sda -At -c "UPDATE local_ega.files SET status = 'COMPLETED', archive_filesize = '$archive_size', archive_file_checksum = '$archive_checksum', archive_file_checksum_type = 'SHA256', decrypted_file_size = '$decrypted_size', decrypted_file_checksum = '$decrypted_checksum', decrypted_file_checksum_type = 'SHA256' WHERE id = 1;")
if [ "$resp" != "UPDATE 1" ]; then
    echo "mark file ready failed"
    exit 1
fi
