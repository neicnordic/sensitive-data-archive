#!/bin/sh
set -e

# install tools if missing
for t in curl jq postgresql-client; do
    if [ ! "$(command -v $t)" ]; then
        if [ "$(id -u)" != 0 ]; then
            echo "$t is missing, unable to install it"
            exit 1
        fi

        apt-get -o DPkg::Lock::Timeout=60 update >/dev/null
        apt-get -o DPkg::Lock::Timeout=60 install -y "$t" >/dev/null
    fi
done

pip -q install s3cmd

cd shared || true

for file in NA12878.bam NA12878_20k_b37.bam; do
    curl -s -L -o /shared/$file "https://github.com/ga4gh/htsget-refserver/raw/main/data/gcp/gatk-test-data/wgs_bam/$file"
    if [ ! -f "$file.c4gh" ]; then
        /shared/crypt4gh encrypt -p c4gh.pub.pem -f "$file"
    fi
    s3cmd -c s3cfg put "$file.c4gh" s3://test_dummy.org/
done

## reupload a file under a different name
s3cmd -c s3cfg put NA12878.bam.c4gh s3://test_dummy.org/NB12878.bam.c4gh

## reupload a file with the same name
s3cmd -c s3cfg put NA12878.bam.c4gh s3://test_dummy.org/

## verify that messages exists in MQ
echo "waiting for upload to complete"
RETRY_TIMES=0
until [ "$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/inbox | jq -r '."messages_ready"')" -eq 4 ]; do
    echo "waiting for upload to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for upload to complete"
        exit 1
    fi
    sleep 2
done

num_rows=$(psql -U postgres -h postgres -d sda -At -c "SELECT COUNT(*) from sda.files;")
if [ "$num_rows" -ne 3 ]; then
    echo "database queries for register_files failed, expected 3 got $num_rows"
    exit 1
fi

num_log_rows=$(psql -U postgres -h postgres -d sda -At -c "SELECT COUNT(*) from sda.file_event_log;")
if [ "$num_log_rows" -ne 8 ]; then
    echo "database queries for file_event_logs failed, expected 8 got $num_log_rows"
    exit 1
fi

## test with token from OIDC service
echo "testing with OIDC token"
newToken=$(curl http://oidc:8080/tokens | jq '.[0]')
sed -i "s/access_token=.*/access_token=$newToken/" s3cfg

s3cmd -c s3cfg put NA12878.bam.c4gh s3://requester_demo.org/data/file1.c4gh

## verify that messages exists in MQ
echo "waiting for upload to complete"
RETRY_TIMES=0
until [ "$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/inbox | jq -r '."messages_ready"')" -eq 5 ]; do
    echo "waiting for upload to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for upload to complete"
        exit 1
    fi
    sleep 2
done

num_rows=$(psql -U postgres -h postgres -d sda -At -c "SELECT COUNT(*) from sda.files;")
if [ "$num_rows" -ne 4 ]; then
    echo "database queries for register_files failed, expected 4 got $num_rows"
    exit 1
fi

num_log_rows=$(psql -U postgres -h postgres -d sda -At -c "SELECT COUNT(*) from sda.file_event_log;")
if [ "$num_log_rows" -ne 10 ]; then
    echo "database queries for file_event_logs failed, expected 10 got $num_log_rows"
    exit 1
fi

echo "files uploaded successfully"
