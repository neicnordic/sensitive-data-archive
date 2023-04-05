#!/bin/sh
set -eou pipefail

export PGPASSWORD=mapper

## map file to dataset
accession="urn:uuid:7964e232-8830-4351-8adb-e4ebb71fafed"
dataset="urn:neic:ci-test-dataset"
file_id=$(psql -U mapper -h postgres -d sda -At -c "SELECT file_id from local_ega.archive_files WHERE stable_id = '$accession';")
if [ "$file_id" -ne 1 ]; then
    echo "get file_id failed"
    exit 1
fi

resp=$(psql -U mapper -h postgres -d sda -At -c "INSERT INTO local_ega_ebi.filedataset (file_id, dataset_stable_id) VALUES ('$file_id', '$dataset') ON CONFLICT DO NOTHING;")
if [ "$resp" != "INSERT 0 1" ]; then
    echo "map to dataset failed"
    exit 1
fi