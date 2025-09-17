#!/bin/sh
set -e

if [ -n "$SYNCTEST" ]; then
    exit 0
fi

cd shared || true

checkStatus () {
	RETRY_TIMES=0
	until [ "$(curl -s -k -H "Authorization: Bearer $token" -X GET http://api:8080/users/test@dummy.org/files | jq | grep -c "$1")" -eq "$2" ]; do
	    echo "waiting for files to become $1"
	    RETRY_TIMES=$((RETRY_TIMES + 1))
	    if [ "$RETRY_TIMES" -eq 30 ]; then
	        echo "::error::Time out while waiting for files to become $1"
	        exit 1
	    fi
	    sleep 2
	done
}

checkErrors() {
	RETRY_TIMES=0
	until [ $(("$(curl -su guest:guest http://rabbitmq:15672/api/queues/sda/error_stream/ | jq -r '.messages_ready')"-"$errorStreamSize")) -eq 1 ]; do
    	echo "checking for $1 error"
    	RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 20 ]; then
        	echo "::error::Time out while waiting for error message"
        	exit 1
    	fi
    	sleep 2
	done
}

# cleanup queues and database
URI=http://rabbitmq:15672
if [ -n "$PGSSLCERT" ]; then
    URI=https://rabbitmq:15671
fi
for q in accession archived backup completed inbox ingest mappings verified rotatekey; do
    curl -s -k -u guest:guest -X DELETE "$URI/api/queues/sda/$q/contents"
done
psql -U postgres -h postgres -d sda -At -c "TRUNCATE TABLE sda.files, sda.encryption_keys CASCADE;"

# register archive and rotation c4gh public keys
token="$(cat /shared/token)"
for keyName in c4gh rotatekey; do
	payload=$(
		jq -c -n \
			--arg description "this is the $keyName key" \
			--arg pubkey "$( base64 -w0 /shared/"$keyName".pub.pem)" \
			'$ARGS.named'
	)
	resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d "$payload" "http://api:8080/c4gh-keys/add")"
	if [ "$resp" != "200" ]; then
		echo "Error when adding the $keyName public key hash, expected 200 got: $resp"
		exit 1
	fi
done

# generate and upload file
file=testfile1
if [ ! -f "$file" ]; then
	dd if=/dev/urandom of="$file" count=10 bs=1M
fi
if [ ! -f "$file.c4gh" ]; then
    yes | /shared/crypt4gh encrypt -p c4gh.pub.pem -f "$file"
fi
s3cmd -c s3cfg put "$file.c4gh" s3://test_dummy.org/dataset_rotatekey/

response="$(curl -s -k -L "http://api:8080/users/test@dummy.org/files" -H "Authorization: Bearer $token" | jq | grep -c dataset_rotatekey)"
if [ "$response" -ne 1 ]; then
	echo "file for rotatekey test failed to upload"
	exit 1
fi

## ingest and map files to dataset
curl -s -k -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d '{"filepath": "dataset_rotatekey/testfile1.c4gh", "user": "test@dummy.org"}' http://api:8080/file/ingest
checkStatus verified 1

curl -s -k -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d '{"accession_id": "ROTATE-KEY-01", "filepath": "dataset_rotatekey/testfile1.c4gh", "user": "test@dummy.org"}' http://api:8080/file/accession
checkStatus ready 1

curl -s -k -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d '{"accession_ids": ["ROTATE-KEY-01"], "dataset_id": "KEY-ROTATION-TEST-0001", "user": "test@dummy.org"}' http://api:8080/dataset/create
checkStatus ready 0

errorStreamSize=$(curl -su guest:guest http://rabbitmq:15672/api/queues/sda/error_stream/ | jq -r '.messages_ready')

## trigger key rotation
properties=$(
    jq -c -n \
        --argjson delivery_mode 2 \
        --arg content_encoding UTF-8 \
        --arg content_type application/json \
        '$ARGS.named'
)

mappings=$(
    jq -c -n \
        '$ARGS.positional' \
        --args "ROTATE-KEY-01"
)

mapping_payload=$(
    jq -r -c -n \
        --arg type mapping \
        --arg dataset_id KEY-ROTATION-TEST-0001 \
        --argjson accession_ids "$mappings" \
        '$ARGS.named|@base64'
)

mapping_body=$(
    jq -c -n \
        --arg vhost test \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "rotatekey" \
        --arg payload_encoding base64 \
        --arg payload "$mapping_payload" \
        '$ARGS.named'
)

curl -s -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$mapping_body" | jq

# check DB for updated key hash in sda.files
rotatekeyHash=$(psql -U postgres -h postgres -d sda -At -c "select key_hash from sda.encryption_keys where description='this is the rotatekey key';")
if "$(psql -U postgres -h postgres -d sda -At -c "select key_hash from sda.files where stable_id like 'ROTATE-KEY-0%';" | grep -c "$rotatekeyHash")" -neq 1;
then
	echo "failed to update the key hash of files"
	exit 1
fi

# check that files were re-verified
echo "waiting for re-verify to complete"
RETRY_TIMES=0
until [ "$(curl -su guest:guest http://rabbitmq:15672/api/queues/sda/archived/ | jq -r '.messages_ready')" -eq 0 ]; do
    echo "waiting for re-verify to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for verify to complete"
        exit 1
    fi
    sleep 2
done

# check that no other erros occured
if [ "$(curl -su guest:guest http://rabbitmq:15672/api/queues/sda/error_stream/ | jq -r '.messages_ready')" -ne "$errorStreamSize" ]; then
	echo "something went wrong with the key rotation"
	exit 1
fi

## download file with rotated key, concatenate header and archive body, decrypt and check
# get rotated header
psql -U postgres -h postgres -d sda -At -c "select header from sda.files where stable_id='ROTATE-KEY-01';" | xxd -r -p > testfile1_rotated.c4gh
# get archive file
archivePath=$(psql -U postgres -h postgres -d sda -At -c "select archive_file_path from sda.files where stable_id='ROTATE-KEY-01';")
s3cmd --access_key=access --secret_key=secretKey --host=minio:9000 --no-ssl --host-bucket=minio:9000 get s3://archive/"$archivePath" --force
# concatenate and decrypt
cat testfile1_rotated.c4gh "$archivePath" > tmp_file && mv tmp_file testfile1_rotated.c4gh
C4GH_PASSPHRASE=rotatekeyPass ./crypt4gh decrypt -f testfile1_rotated.c4gh -s rotatekey.sec.pem

# check that decrypted file matches the original
if [ ! -f "testfile1_rotated" ]; then
    echo "decrypted file testfile1_rotated not found"
    exit 1
fi
if ! cmp -s "testfile1_rotated" "testfile1" ; then
   echo "downloaded file is different from the original one"
   exit 1
fi
# compare hashes as well
if [ "$(sha256sum testfile1 | cut -d ' ' -f 1)" != "$(sha256sum testfile1 | cut -d ' ' -f 1)" ]; then
	echo "downloaded file has different sha256 hash from the original one"
	exit 1
fi

### test for errors ###

# multiple accession_id's per message is not supported
mappings=$(
    jq -c -n \
        '$ARGS.positional' \
        --args "ROTATE-KEY-01" \
        --args "ROTATE-KEY-02"
)

mapping_payload=$(
    jq -r -c -n \
        --arg type mapping \
        --arg dataset_id KEY-ROTATION-TEST-0001 \
        --argjson accession_ids "$mappings" \
        '$ARGS.named|@base64'
)

mapping_body=$(
    jq -c -n \
        --arg vhost test \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "rotatekey" \
        --arg payload_encoding base64 \
        --arg payload "$mapping_payload" \
        '$ARGS.named'
)

curl -s -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$mapping_body" | jq

checkErrors "multiple accession_id's per message is not supported"
errorStreamSize=$(curl -su guest:guest http://rabbitmq:15672/api/queues/sda/error_stream/ | jq -r '.messages_ready')

# rotation key is deprecated
rotateKeyHash=$(cat /shared/rotatekey.pub.pem | awk 'NR==2' | base64 -d | xxd -p -c256)
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST "http://api:8080/c4gh-keys/deprecate/$rotateKeyHash")"
if [ "$resp" != "200" ]; then
	echo "Error when trying to deprecate rotation public key hash, expected 200 got: $resp"
	exit 1
fi

curl -s -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$mapping_body" | jq

checkErrors "rotation key is deprecated"
errorStreamSize=$(curl -su guest:guest http://rabbitmq:15672/api/queues/sda/error_stream/ | jq -r '.messages_ready')

echo "Rotate key integration tests completed successfully"
