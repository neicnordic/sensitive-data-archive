#!/bin/sh
set -e

if [ -n "$SYNCTEST" ]; then
    exit 0
fi

cd shared || true

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

checkConsumers() {
    RETRY_TIMES=0
    until [ "$(curl -su guest:guest http://localhost:15672/api/consumers | jq '.[].queue.name' | grep -c "$1")" -eq "$2" ]; do
        echo "waiting for $1 consumer status"
        RETRY_TIMES=$((RETRY_TIMES + 1))
        if [ "$RETRY_TIMES" -eq 30 ]; then
            echo "::error::Time out while waiting for $1 consumer status"
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
psql -U postgres -h postgres -d sda -At -c "TRUNCATE TABLE sda.files, sda.encryption_keys, sda.datasets, sda.file_dataset CASCADE;"

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

# generate and upload two files for dataset
for i in 1 2; do
    file="testfile$i"
    if [ ! -f "$file" ]; then
        dd if=/dev/urandom of="$file" count=5 bs=1M
    fi
    if [ ! -f "$file.c4gh" ]; then
        yes | /shared/crypt4gh encrypt -p c4gh.pub.pem -f "$file"
    fi
    s3cmd -c s3cfg put "$file.c4gh" s3://test_dummy.org/dataset_admin_rotatekey/
done

# wait for files to appear in user files
RETRY_TIMES=0
until [ "$(curl -s -k -L "http://api:8080/users/test@dummy.org/files" -H "Authorization: Bearer $token" | jq | grep -c dataset_admin_rotatekey)" -eq 2 ]; do
    echo "waiting for files to upload"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for files to upload"
        exit 1
    fi
    sleep 2
done

## ingest both files
for i in 1 2; do
    curl -s -k -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d "{\"filepath\": \"dataset_admin_rotatekey/testfile$i.c4gh\", \"user\": \"test@dummy.org\"}" http://api:8080/file/ingest
done

# wait for files to become verified
RETRY_TIMES=0
until [ "$(curl -s -k -H "Authorization: Bearer $token" -X GET http://api:8080/users/test@dummy.org/files | jq | grep -c "verified")" -eq 2 ]; do
    echo "waiting for files to become verified"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for files to become verified"
        exit 1
    fi
    sleep 2
done

# assign accession IDs to the files
for i in 1 2; do
    fileID=$(psql -U postgres -h postgres -d sda -At -c "select id from sda.files where submission_file_path='dataset_admin_rotatekey/testfile$i.c4gh';")
    payload=$(
        jq -c -n \
            --arg accession_id "EGAF123456789$i" \
            --arg file_id "$fileID" \
            '$ARGS.named'
    )
    resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d "$payload" "http://api:8080/file/accession")"
    if [ "$resp" != "200" ]; then
        echo "Error when assigning accession ID to file $i, expected 200 got: $resp"
        exit 1
    fi
done

# create dataset with both files
dataset_payload=$(
    jq -c -n \
        --arg dataset_id "EGAD12345678901" \
        --arg user "test@dummy.org" \
        --argjson accession_ids '["EGAF1234567891", "EGAF1234567892"]' \
        '$ARGS.named'
)
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d "$dataset_payload" "http://api:8080/dataset/create")"
if [ "$resp" != "200" ]; then
    echo "Error when creating dataset, expected 200 got: $resp"
    exit 1
fi

# verify dataset was created and contains both files
dataset_files_count="$(curl -s -k -H "Authorization: Bearer $token" -X GET "http://api:8080/dataset/EGAD12345678901/fileids" | jq '. | length')"
if [ "$dataset_files_count" -ne 2 ]; then
    echo "Expected 2 files in dataset, got: $dataset_files_count"
    exit 1
fi

echo "Dataset created successfully with 2 files"

# get initial error stream size for error checking
errorStreamSize=$(curl -su guest:guest http://rabbitmq:15672/api/queues/sda/error_stream/ | jq -r '.messages_ready')

# get file IDs for verification later
fileID1=$(psql -U postgres -h postgres -d sda -At -c "select id from sda.files where submission_file_path='dataset_admin_rotatekey/testfile1.c4gh';")
fileID2=$(psql -U postgres -h postgres -d sda -At -c "select id from sda.files where submission_file_path='dataset_admin_rotatekey/testfile2.c4gh';")

echo "File IDs: $fileID1, $fileID2"

## test dataset key rotation via sda-admin CLI tool
echo "Testing dataset key rotation via sda-admin CLI tool"

# Use sda-admin CLI tool to rotate keys for the dataset
SDA_ADMIN_API_URI="http://api:8080" SDA_ADMIN_TOKEN="$token" /shared/sda-admin dataset rotatekey -dataset-id "EGAD12345678901"

if [ $? -ne 0 ]; then
    echo "Error when running sda-admin dataset rotatekey command"
    exit 1
fi

echo "Dataset key rotation command executed successfully"

# check DB for updated key hash in sda.files for both files
rotatekeyHash=$(psql -U postgres -h postgres -d sda -At -c "select key_hash from sda.encryption_keys where description='this is the rotatekey key';")
echo "Expected rotate key hash: $rotatekeyHash"

# wait for key rotation to complete and verify both files have new key hash
RETRY_TIMES=0
until [ "$(psql -U postgres -h postgres -d sda -At -c "select key_hash from sda.files where id='$fileID1';")" = "$rotatekeyHash" ] && [ "$(psql -U postgres -h postgres -d sda -At -c "select key_hash from sda.files where id='$fileID2';")" = "$rotatekeyHash" ]; do
    echo "waiting for key rotation to complete for both files"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 60 ]; then
        echo "::error::Time out while waiting for key rotation to complete"
        echo "File 1 key hash: $(psql -U postgres -h postgres -d sda -At -c "select key_hash from sda.files where id='$fileID1';")"
        echo "File 2 key hash: $(psql -U postgres -h postgres -d sda -At -c "select key_hash from sda.files where id='$fileID2';")"
        exit 1
    fi
    sleep 2
done

echo "Key rotation completed for both files"

# check that files were re-verified (archived queue should be empty)
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

# check that no errors occurred during the process
sleep 5
if [ "$(curl -su guest:guest http://rabbitmq:15672/api/queues/sda/error_stream/ | jq -r '.messages_ready')" -ne "$errorStreamSize" ]; then
    echo "something went wrong with the dataset key rotation"
    exit 1
fi

echo "Re-verification completed successfully"

## verify both files can be downloaded and decrypted with rotated key
for i in 1 2; do
    fileID_var="fileID$i"
    eval fileID=\$$fileID_var
    
    echo "Testing download and decryption of file $i (ID: $fileID)"
    
    # get rotated header
    psql -U postgres -h postgres -d sda -At -c "select header from sda.files where id='$fileID';" | xxd -r -p > "testfile${i}_rotated.c4gh"
    
    # get archive file
    archivePath=$(psql -U postgres -h postgres -d sda -At -c "select archive_file_path from sda.files where id='$fileID';")
    s3cmd --access_key=access --secret_key=secretKey --host=minio:9000 --no-ssl --host-bucket=minio:9000 get s3://archive/"$archivePath" --force
    
    # concatenate and decrypt
    cat "testfile${i}_rotated.c4gh" "$archivePath" > tmp_file && mv tmp_file "testfile${i}_rotated.c4gh"
    C4GH_PASSPHRASE=rotatekeyPass ./crypt4gh decrypt -f "testfile${i}_rotated.c4gh" -s rotatekey.sec.pem
    
    # check that decrypted file matches the original
    if [ ! -f "testfile${i}_rotated" ]; then
        echo "decrypted file testfile${i}_rotated not found"
        exit 1
    fi
    if ! cmp -s "testfile${i}_rotated" "testfile$i" ; then
       echo "downloaded file $i is different from the original one"
       exit 1
    fi
    # compare hashes as well
    if [ "$(sha256sum testfile$i | cut -d ' ' -f 1)" != "$(sha256sum testfile${i}_rotated | cut -d ' ' -f 1)" ]; then
        echo "downloaded file $i has different sha256 hash from the original one"
        exit 1
    fi
    
    echo "File $i download and decryption successful"
done

### test for errors ###

## test dataset key rotation with non-existent dataset
echo "Testing dataset key rotation with non-existent dataset"

SDA_ADMIN_API_URI="http://api:8080" SDA_ADMIN_TOKEN="$token" /shared/sda-admin dataset rotatekey -dataset-id "NONEXISTENT" 2>/dev/null
if [ $? -eq 0 ]; then
    echo "Expected sda-admin to fail with non-existent dataset, but it succeeded"
    exit 1
fi

## test getting dataset file IDs via API
echo "Testing dataset file IDs endpoint"

dataset_fileids="$(curl -s -k -H "Authorization: Bearer $token" -X GET "http://api:8080/dataset/EGAD12345678901/fileids")"
fileids_count="$(echo "$dataset_fileids" | jq '. | length')"
if [ "$fileids_count" -ne 2 ]; then
    echo "Expected 2 file IDs, got: $fileids_count"
    exit 1
fi

# verify the file IDs contain both fileID and accessionID
file1_has_fileid="$(echo "$dataset_fileids" | jq -r '.[0].fileID' | grep -c "$fileID1" || true)"
file1_has_accession="$(echo "$dataset_fileids" | jq -r '.[0].accessionID' | grep -c "EGAF123456789" || true)"
if [ "$file1_has_fileid" -ne 1 ] || [ "$file1_has_accession" -ne 1 ]; then
    echo "File IDs endpoint doesn't return correct structure"
    echo "Response: $dataset_fileids"
    exit 1
fi

echo "Dataset file IDs endpoint working correctly"

printf "\033[32mDataset admin CLI rotate key integration tests completed successfully\033[0m\n"
