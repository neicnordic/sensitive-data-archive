#!/bin/sh
set -eou pipefail

export PGPASSWORD=download
dataset="urn:neic:ci-test-dataset"
accession="urn:uuid:7964e232-8830-4351-8adb-e4ebb71fafed"

for host in migrate postgres; do
    ## get file info
    resp=$(psql -U download -h "$host" -d sda -At -c "SELECT a.file_id, dataset_id, display_file_name, file_name, file_size, decrypted_file_size, decrypted_file_checksum, decrypted_file_checksum_type, file_status from local_ega_ebi.file a, local_ega_ebi.file_dataset b WHERE dataset_id = '$dataset' AND a.file_id=b.file_id;")
    if [ "$(echo "$resp" | cut -d '|' -f1)" != "$accession" ]; then
        echo "get file info failed"
        exit 1
    fi

    ## check dataset
    resp=$(psql -U download -h "$host" -d sda -At -c "SELECT DISTINCT dataset_id FROM local_ega_ebi.file_dataset WHERE dataset_id = '$dataset'")
    if [ "$resp" != "$dataset" ]; then
        echo "check dataset failed"
        exit 1
    fi

    ## check file permissions
    resp=$(psql -U download -h "$host" -d sda -At -c "SELECT dataset_id FROM local_ega_ebi.file_dataset WHERE file_id = '$accession'")
    if [ "$resp" != "$dataset" ]; then
        echo "check file permissions failed"
        exit 1
    fi

    ## get file
    archive_path=d853c51b-6aed-4243-b427-177f5e588857
    resp=$(psql -U download -h "$host" -d sda -At -c "SELECT file_path, archive_file_size, header FROM local_ega_ebi.file WHERE file_id = '$accession'")
    if [ "$(echo "$resp" | cut -d '|' -f1)" != "$archive_path" ]; then
        echo "get file failed"
        exit 1
    fi
done

echo "70_download_queries completed successfully"
