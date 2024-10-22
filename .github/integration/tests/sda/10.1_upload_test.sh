#!/bin/sh
set -e

if [ -z "$STORAGETYPE" ]; then
    echo "STORAGETYPE not set, exiting"
    exit 1
fi

if [ "$STORAGETYPE" = "s3" ]; then
    exit 0
fi

for t in curl jq openssh-client postgresql-client; do
    if [ ! "$(command -v $t)" ]; then
        if [ "$(id -u)" != 0 ]; then
            echo "$t is missing, unable to install it"
            exit 1
        fi

        apt-get -o DPkg::Lock::Timeout=60 update >/dev/null
        apt-get -o DPkg::Lock::Timeout=60 install -y "$t" >/dev/null
    fi
done

cd shared || true

## verify that messages exists in MQ
URI=http://rabbitmq:15672
if [ -n "$PGSSLCERT" ]; then
    URI=https://rabbitmq:15671
fi
## empty all queues ##
for q in accession archived backup completed inbox ingest mappings verified; do
    curl -s -k -u guest:guest -X DELETE "$URI/api/queues/sda/$q/contents"
done
## truncate database
psql -U postgres -h postgres -d sda -At -c "TRUNCATE TABLE sda.files CASCADE;"

if [ "$STORAGETYPE" = "posix" ]; then
    for file in NA12878.bam NA12878_20k_b37.bam NA12878.bai NA12878_20k_b37.bai; do
        echo "downloading $file"
        curl --retry 100 -s -L -o /shared/$file "https://github.com/ga4gh/htsget-refserver/raw/main/data/gcp/gatk-test-data/wgs_bam/$file"
        if [ ! -f "$file.c4gh" ]; then
            yes | /shared/crypt4gh encrypt -p c4gh.pub.pem -f "$file"
        fi

        sftp -i /shared/keys/ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -o KbdInteractiveAuthentication=no -o User=dummy@example.com -P 2222 inbox <<-EOF
    put "${file}"
    dir
    ls -al
    exit
EOF
    done

    ## reupload a file under a different name
    sftp -i /shared/keys/ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -o KbdInteractiveAuthentication=no -o User=dummy@example.com -P 2222 inbox <<-EOF
    put NA12878.bam.c4gh NB12878.bam.c4gh
    dir
    ls -al
    exit
EOF

    ## reupload a file with the same name
    sftp -i /shared/keys/ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -o KbdInteractiveAuthentication=no -o User=dummy@example.com -P 2222 inbox <<-EOF
    put NA12878.bam.c4gh
    dir
    ls -al
    exit
EOF

fi

echo "waiting for upload to complete"
RETRY_TIMES=0
until [ "$(curl -s -k -u guest:guest $URI/api/queues/sda/inbox | jq -r '."messages_ready"')" -eq 6 ]; do
    echo "waiting for upload to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for upload to complete"
        exit 1
    fi
    sleep 2
done


echo "files uploaded successfully"
