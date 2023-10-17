#!/bin/sh
set -eou pipefail

export PGPASSWORD=mapper

## map file to dataset
accession="urn:uuid:7964e232-8830-4351-8adb-e4ebb71fafed"
dataset="urn:neic:ci-test-dataset"

for host in migrate postgres; do
    file_id=$(psql -U mapper -h "$host" -d sda -At -c "SELECT id FROM sda.files WHERE stable_id = '$accession';")
    if [ -z "$file_id" ]; then
        echo "get file_id failed"
        exit 1
    fi

    dataset_id=$(psql -U mapper -h "$host" -d sda -At -c "INSERT INTO sda.datasets (stable_id) VALUES ('$dataset') ON CONFLICT DO NOTHING;")
    if [ "$dataset_id" != "INSERT 0 1" ]; then
        echo "insert dataset failed"
        exit 1
    fi

    resp=$(psql -U mapper -h "$host" -d sda -At -c "INSERT INTO sda.file_dataset (file_id, dataset_id) SELECT '$file_id', id FROM sda.datasets WHERE stable_id = '$dataset' ON CONFLICT DO NOTHING;")
    if [ "$resp" != "INSERT 0 1" ]; then
        echo "map file to dataset failed"
        exit 1
    fi

    register=$(psql -U mapper -h "$host" -d sda -At -c "INSERT INTO sda.dataset_event_log(dataset_id, event, message) VALUES('$dataset', 'registered', '{\"type\": \"mapping\"}');")
    if [ "$register" != "INSERT 0 1" ]; then
        echo "update dataset event failed"
        exit 1
    fi

    release=$(psql -U mapper -h "$host" -d sda -At -c "INSERT INTO sda.dataset_event_log(dataset_id, event, message) VALUES('$dataset', 'released', '{\"type\": \"release\"}');")
    if [ "$release" != "INSERT 0 1" ]; then
        echo "update dataset event failed"
        exit 1
    fi

    deprecate=$(psql -U mapper -h "$host" -d sda -At -c "INSERT INTO sda.dataset_event_log(dataset_id, event, message) VALUES('$dataset', 'deprecated', '{\"type\": \"deprecate\"}');")
    if [ "$deprecate" != "INSERT 0 1" ]; then
        echo "update dataset event failed"
        exit 1
    fi
done