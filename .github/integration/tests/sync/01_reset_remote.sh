#!/bin/bash
set -e

cd shared || true

if [ "$STORAGETYPE" = "posix" ]; then
    exit 0
fi

# install tools if missing
for t in curl expect jq openssh-client postgresql-client; do
    if [ ! "$(command -v $t)" ]; then
        if [ "$(id -u)" != 0 ]; then
            echo "$t is missing, unable to install it"
            exit 1
        fi

        apt-get -o DPkg::Lock::Timeout=60 update >/dev/null
        apt-get -o DPkg::Lock::Timeout=60 install -y "$t" >/dev/null
    fi
done

for q in accession archived catch_all.dead completed inbox ingest mappings sync_files sync_datasets verified; do
    curl -s -k -u guest:guest -X DELETE "http://rabbitmq:15672/api/queues/sda/$q/contents"
    curl -s -k -u guest:guest -X DELETE "http://remote-rabbitmq:15672/api/queues/sda/$q/contents"
done

## truncate database
psql -U postgres -h remote-postgres -d sda -At -c "TRUNCATE TABLE sda.files CASCADE;"