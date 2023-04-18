#!/bin/sh
set -eou pipefail

result=$(psql -U postgres -h postgres -d sda -At -c "SELECT submission_file_path,submission_user from sda.files;")
path=$(echo "$result" | cut -d '|' -f1)
user=$(echo "$result" | cut -d '|' -f2)

export PGPASSWORD=ingest

## insert file
resp=$(psql -U ingest -h postgres -d sda -At -c  "UPDATE local_ega.main SET status = 'IN_INGESTION' WHERE local_ega.main.submission_file_path = '$path' AND local_ega.main.submission_user = '$user' AND local_ega.main.status = 'INIT' RETURNING id;")
if [ "$(echo "$resp" | tr -d '\n')" != "1UPDATE 1" ]; then
    echo "insert file failed"
    exit 1
fi

## store header
resp=$(psql -U ingest -h postgres -d sda -At -c "UPDATE local_ega.files SET header = '637279707434676801000000010000006c00000000000000' WHERE id = 1;")
if [ "$resp" != "UPDATE 1" ]; then
    echo "store header failed"
    exit 1
fi
## set archived
archive_path=d853c51b-6aed-4243-b427-177f5e588857
size="2035150"
checksum="f03775a50feea74c579d459fdbeb27adafd543b87f6692703543a6ebe7daa1ff"
resp=$(psql -U ingest -h postgres -d sda -At -c "UPDATE local_ega.files SET status = 'ARCHIVED', archive_path = '$archive_path', archive_filesize = '$size', inbox_file_checksum = '$checksum', inbox_file_checksum_type = 'SHA256' WHERE id = 1;")
if [ "$resp" != "UPDATE 1" ]; then
    echo "mark file archived failed"
    exit 1
fi
