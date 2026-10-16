#!/bin/bash
set -e
cd shared || true

admin_token="$(cat /shared/token)"
user_token=$(grep 'access_token' /shared/s3cfg | sed -E 's/access_token="?([^"]+)"?/\1/')
# Upload a file and make sure it's listed
result="$(curl -sk -L "http://api:8080/users/test@dummy.org/files" -H "Authorization: Bearer $admin_token" | jq '. | length')"
if [ "$result" -ne 2 ]; then
    echo "wrong number of files returned for user test@dummy.org"
    echo "expected 2 got $result"
    exit 1
fi

## trigger re-verification of EGAF74900000001
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $admin_token" -X PUT "http://api:8080/file/verify/EGAF74900000001")"
if [ "$resp" != "200" ]; then
	echo "Error when starting re-verification, expected 200 got: $resp"
	exit 1
fi

## failure on wrong accession ID
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $admin_token" -X PUT "http://api:8080/file/verify/EGAF74900054321")"
if [ "$resp" != "404" ]; then
	echo "Error when starting re-verification, expected 404 got: $resp"
	exit 1
fi

## trigger re-verification of dataset SYNC-001-12345
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $admin_token" -X PUT "http://api:8080/dataset/verify/SYNC-001-12345")"
if [ "$resp" != "200" ]; then
	echo "Error when starting re-verification of dataset, expected 200 got: $resp"
	exit 1
fi

## expect failure of missing dataset
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $admin_token" -X PUT "http://api:8080/dataset/verify/SYNC-999-12345")"
if [ "$resp" != "404" ]; then
	echo "Error when starting re-verification of missing dataset, expected 404 got: $resp"
	exit 1
fi

# Reupload a file under a different name, test to delete it
s3cmd -c s3cfg put "NA12878.bam.c4gh" s3://test_dummy.org/NC12878.bam.c4gh
stream_size=$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/inbox | jq '.messages_ready')

URI=http://rabbitmq:15672
if [ -n "$PGSSLCERT" ]; then
   URI=https://rabbitmq:15671
fi
stream_size=$((stream_size + 1))
RETRY_TIMES=0

until [ $((stream_size)) -eq "$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/inbox | jq '.messages_ready')" ]; do
    echo "waiting for upload to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "Upload did not complete successfully"
        exit 1
    fi
    sleep 2
done

# get the fileId of the new file
fileid="$(curl -k -L -H "Authorization: Bearer $admin_token" "http://api:8080/users/test@dummy.org/files" | jq -r '.[] | select(.inboxPath == "NC12878.bam.c4gh") | .fileID')"

output=$(s3cmd -c s3cfg ls s3://test_dummy.org/NC12878.bam.c4gh 2>/dev/null)
if [ -z "$output" ] ; then
    echo "Uploaded file not in inbox"
    exit 1
fi
# delete it
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $admin_token" -X DELETE "http://api:8080/file/test@dummy.org/$fileid")"
if [ "$resp" != "200" ]; then
    echo "Error when deleting the file, expected 200 got: $resp"
    exit 1
fi

last_event=$(psql -U postgres -h postgres -d sda -At -c "SELECT event FROM sda.file_event_log WHERE file_id='$fileid' order by started_at desc limit 1;")

if [ "$last_event" != "removed" ]; then
   echo "The file $fileid does not have the expected last event 'removed', but $last_event."
   exit 1
fi

output=$(s3cmd -c s3cfg ls s3://test_dummy.org/NC12878.bam.c4gh 2>/dev/null)
if [ -n "$output" ] ; then
    echo "Deleted file is still in inbox"
    exit 1
fi

assert_file_listing() {
    local endpoint=$1 auth_token=$2 file_id=$3 expected=$4 status=${5:-}
    local count

    count="$(curl -s -k -L "http://api:8080$endpoint" \
        -H "Authorization: Bearer $auth_token" |
        jq --arg id "$file_id" --arg status "$status" \
            '[.[] | select(.fileID == $id and ($status == "" or .fileStatus == $status))] | length')"

    if [ "$count" -ne "$expected" ]; then
        echo "Expected $expected matching file(s) from $endpoint; got $count"
        return 1
    fi
}

assert_file_listing "/files" "$user_token" "$fileid" 0
assert_file_listing "/users/test@dummy.org/files" "$admin_token" "$fileid" 0

# Try to delete with invalid file id
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $admin_token" -X DELETE "http://api:8080/file/test@dummy.org/badfileid")"
if [ "$resp" != "400" ]; then
    echo "Error when deleting the file, expected 400 error got: $resp"
    exit 1
fi

# Try to delete with unknown file id
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $admin_token" -X DELETE "http://api:8080/file/test@dummy.org/00000000-0000-0000-0000-000000000000")"
if [ "$resp" != "404" ]; then
    echo "Error when deleting the file, expected 404 error got: $resp"
    exit 1
fi


# Try to delete file of other user
fileid="$(curl -k -L -H "Authorization: Bearer $admin_token" "http://api:8080/users/testu@lifescience-ri.eu/files" | jq -r '.[0]| .fileID')"
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $admin_token" -X DELETE "http://api:8080/file/test@dummy.org/$fileid")"
if [ "$resp" != "404" ]; then
    echo "Error when deleting the file, expected 404 got: $resp"
    exit 1
fi


# Re-upload the file and use the api to ingest it, then try to delete it
s3cmd -c s3cfg put NA12878.bam.c4gh s3://test_dummy.org/NE12878.bam.c4gh
stream_size=$((stream_size + 1))
RETRY_TIMES=0

until [ $((stream_size)) -eq "$(curl -s -u guest:guest $URI/api/queues/sda/inbox | jq '.messages_ready')" ]; do
    echo "waiting for upload to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "Upload did not complete successfully"
        exit 1
    fi
    sleep 2
done

# Ingest it
new_payload=$(
jq -c -n \
	--arg filepath "NE12878.bam.c4gh" \
	--arg user "test@dummy.org" \
	'$ARGS.named'
)

resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $admin_token" -H "Content-Type: application/json" -X POST -d "$new_payload" "http://api:8080/file/ingest")"
if [ "$resp" != "200" ]; then
    echo "Error when requesting to ingesting file, expected 200 got: $resp"
    exit 1
fi

fileid="$(curl -k -L -H "Authorization: Bearer $admin_token" "http://api:8080/users/test@dummy.org/files" | jq -r '.[] | select(.inboxPath == "NE12878.bam.c4gh") | .fileID')"
# wait for the fail to get the correct status
RETRY_TIMES=0

until [ "$(psql -U postgres -h postgres -d sda -At -c "SELECT event FROM sda.file_event_log WHERE file_id='$fileid' order by started_at desc limit 1;")" = "verified" ]; do
   echo "waiting for ingest and verification to complete for file: $fileid"
   RETRY_TIMES=$((RETRY_TIMES + 1))
   if [ "$RETRY_TIMES" -eq 30 ]; then
      echo "::error::Time out while waiting for file: $fileid ingest and verification to complete"
      exit 1
   fi
   sleep 2
done

# Finalize file
echo "Giving accession id by using json payload"
accession_payload=$(
jq -c -n \
	--arg filepath "NE12878.bam.c4gh" \
	--arg user "test@dummy.org" \
    --arg accession_id "my-id-01" \
	'$ARGS.named'
)

resp_accession_payload="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $admin_token" -H "Content-Type: application/json" -X POST -d "$accession_payload" "http://api:8080/file/accession")"
if [ "$resp_accession_payload" != "200" ]; then
    echo "Error when requesting to set accession id to file, expected 200 got: $resp_accession_payload"
    exit 1
fi

# Check that the file has been finalized
RETRY_TIMES=0
until [ "$(psql -U postgres -h postgres -d sda -At -c "SELECT event FROM sda.file_event_log WHERE file_id='$fileid' order by started_at desc limit 1;")" = "ready" ]; do
   echo "waiting for finalize to complete"
   RETRY_TIMES=$((RETRY_TIMES + 1))
   if [ "$RETRY_TIMES" -eq 10 ]; then
      echo "::error::Time out while waiting for finalizing to complete"
      exit 1
   fi
   sleep 2
done
echo "Finalize by using json payload finished successfully"


# Test canceling a file that has already been mapped to a dataset should fail
dataset_create_payload=$(
jq -c -n \
	--argjson accession_ids '["my-id-01"]' \
	--arg dataset_id "cancel-test-dataset" \
	'$ARGS.named'
)
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $admin_token" -H "Content-Type: application/json" -X POST -d "$dataset_create_payload" "http://api:8080/dataset/create")"
if [ "$resp" != "200" ]; then
    echo "Error when creating dataset, expected 200 got: $resp"
    exit 1
fi

# wait for the mapper to add the file to the dataset
RETRY_TIMES=0
until [ "$(psql -U postgres -h postgres -d sda -At -c "select count(id) from sda.file_dataset where dataset_id = (select id from sda.datasets where stable_id = 'cancel-test-dataset');")" -eq 1 ]; do
    echo "waiting for dataset mapping to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for dataset mapping to complete"
        exit 1
    fi
    sleep 2
done

already_mapped_payload=$(
jq -c -n \
	--arg filepath "NE12878.bam.c4gh" \
	--arg user "test@dummy.org" \
	'$ARGS.named'
)
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $admin_token" -H "Content-Type: application/json" -X POST -d "$already_mapped_payload" "http://api:8080/file/cancel")"
if [ "$resp" != "409" ]; then
    echo "Error when cancelling a file that has already been mapped to a dataset, expected 409 got: $resp"
    exit 1
fi
echo "Testing cancelation of files already mapped to a dataset finished successfully"

# Test canceling a file that is uploaded but not mapped to a dataset yet
cancelfile="cancelingest.bam"
s3cmd -c s3cfg put NA12878.bam.c4gh s3://test_dummy.org/$cancelfile.c4gh
stream_size=$((stream_size + 1))
RETRY_TIMES=0

until [ $((stream_size)) -eq "$(curl -s -u guest:guest $URI/api/queues/sda/inbox | jq '.messages_ready')" ]; do
    echo "waiting for upload to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "Upload did not complete successfully"
        exit 1
    fi
    sleep 2
done

cancel_test_payload=$(
jq -c -n \
	--arg filepath "$cancelfile.c4gh" \
	--arg user "test@dummy.org" \
	'$ARGS.named'
)

# Ingest the file
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $admin_token" -H "Content-Type: application/json" -X POST -d "$cancel_test_payload" "http://api:8080/file/ingest")"
if [ "$resp" != "200" ]; then
    echo "Error when requesting to ingest file, expected 200 got: $resp"
    exit 1
fi

fileid="$(curl -k -L -H "Authorization: Bearer $admin_token" "http://api:8080/users/test@dummy.org/files" | jq -r ".[] | select(.inboxPath == \"$cancelfile.c4gh\") | .fileID")"

# Wait until the file is in the archive
RETRY_TIMES=0
until [ "$(psql -U postgres -h postgres -d sda -At -c "SELECT event FROM sda.file_event_log WHERE file_id='$fileid' order by started_at desc limit 1;")" = "verified" ]; do
   echo "waiting for verified to complete"
   RETRY_TIMES=$((RETRY_TIMES + 1))
   if [ "$RETRY_TIMES" -eq 10 ]; then
      echo "::error::Time out while waiting for verified to complete"
      exit 1
   fi
   sleep 2
done
echo "File ingested successfully"



# Cancel the ingestion using the same payload that was used to start it
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $admin_token" -H "Content-Type: application/json" -X POST -d "$cancel_test_payload" "http://api:8080/file/cancel")"
if [ "$resp" != "200" ]; then
    echo "Error when requesting to cancel file, expected 200 got: $resp"
    exit 1
fi
echo "Testing cancelation of uploaded file finished successfully"

# Check that the file has been disabled
RETRY_TIMES=0
until [ "$(psql -U postgres -h postgres -d sda -At -c "SELECT event FROM sda.file_event_log WHERE file_id='$fileid' order by started_at desc limit 1;")" = "disabled" ]; do
   echo "waiting for cancellation to complete"
   RETRY_TIMES=$((RETRY_TIMES + 1))
   if [ "$RETRY_TIMES" -eq 30 ]; then
      echo "::error::Time out while waiting for file: $fileid cancellation to complete"
      exit 1
   fi
   sleep 2
done

# Check that the archive location and path have been unset
if [ "$(psql -U postgres -h postgres -d sda -At -c "SELECT 1 FROM sda.files WHERE id='$fileid' AND archive_file_path = '' AND archive_location IS NULL")" != "1" ]; then
    echo "cancelling file via API failed, archive data not cleared"
    exit 1
fi


# Verify that the disabled file is still listed in the API responses
assert_file_listing "/files" "$user_token" "$fileid" 1 disabled
assert_file_listing "/users/test@dummy.org/files" "$admin_token" "$fileid" 1 disabled

# Try to delete file not in inbox
fileid="$(curl -k -L -H "Authorization: Bearer $admin_token" "http://api:8080/users/test@dummy.org/files" | jq -r '.[] | select(.inboxPath == "NE12878.bam.c4gh") | .fileID')"
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $admin_token" -X DELETE "http://api:8080/file/test@dummy.org/$fileid")"
if [ "$resp" != "404" ]; then
	echo "Error when deleting the file, expected 404 got: $resp"
	exit 1
fi

# Test downloading the file from the inbox
# Reupload a file under a different name, test to download it
s3cmd -c s3cfg put "NA12878.bam.c4gh" s3://test_dummy.org/NC12878.bam.c4gh
fileid="$(curl -k -L -H "Authorization: Bearer $admin_token" "http://api:8080/users/test@dummy.org/files" | jq -r '.[] | select(.inboxPath == "NC12878.bam.c4gh") | .fileID')"
output=$(s3cmd -c s3cfg ls s3://test_dummy.org/NC12878.bam.c4gh 2>/dev/null)
if [ -z "$output" ] ; then
    echo "Uploaded file NC12878.bam.c4gh not found in inbox"
    exit 1
fi

# download the file, re-encrypted with the client key
clientPubKey="$(base64 -w0 /shared/client.pub.pem)"
resp="$(curl -s -k -L -w "%{http_code}\n" -H "Authorization: Bearer $admin_token" -H "C4GH-Public-Key: $clientPubKey" "http://api:8080/users/test@dummy.org/file/$fileid" -JO)"
if [ "$resp" != "200" ]; then
    echo "Error when downloading the file, expected 200 got: $resp"
    exit 1
fi

# decrypt the downloaded file
export C4GH_PASSPHRASE=c4ghpass
if [ ! -f "NC12878.bam.c4gh" ]; then
    echo "downloaded file NC12878.bam.c4gh not found"
    exit 1
fi
if [ ! -f "/shared/client.sec.pem" ]; then
    echo "client key not found"
    exit 1
fi

# copy the file to avoid overwriting the original
if ! /shared/crypt4gh decrypt -f "NC12878.bam.c4gh" -s "/shared/client.sec.pem" ; then
    echo "decrypting file NC12878.bam.c4gh failed"
    exit 1
fi

# check the file content
if [ ! -f "NC12878.bam" ]; then
    echo "decrypted file NC12878.bam not found"
    exit 1
fi
if ! cmp -s "NC12878.bam" "NA12878.bam" ; then
   echo "downloaded file is different from the original one"
   exit 1
fi

# download file as a non admin user should fail
resp="$(curl -s -k -L -w "%{http_code}\n" -H "Authorization: Bearer $user_token" -H "C4GH-Public-Key: $clientPubKey" "http://api:8080/users/test@dummy.org/file/$fileid" -JO)"
if [ "$resp" != "401" ]; then
    echo "Error when downloading the file, expected 401 got: $resp"
    exit 1
fi

# download a non existing file should fail
nonExistingId="00000000-0000-0000-0000-000000000000"
resp="$(curl -s -k -L -w "%{http_code}\n" -H "Authorization: Bearer $admin_token" -H "C4GH-Public-Key: $clientPubKey" "http://api:8080/users/test@dummy.org/file/$nonExistingId" -JO)"
if [ "$resp" != "404" ]; then
    echo "Trying to download a non existing file, expected 404 got: $resp"
    exit 1
fi

# Test ingesting file by using the file id
echo "Ingest file by using file ID"
# Reupload a file under a different name
s3cmd -c s3cfg put NA12878.bam.c4gh s3://test_dummy.org/ingest/NB12878-ingest.bam.c4gh
sleep 3
# Find the file id of the uploaded file
new_fileid="$(curl -k -L -H "Authorization: Bearer $admin_token" "http://api:8080/users/test@dummy.org/files" | jq -r '.[] | select(.inboxPath == "ingest/NB12878-ingest.bam.c4gh") | .fileID')"
# ingest the file
ingest_resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $admin_token" -X POST "http://api:8080/file/ingest?fileid=$new_fileid")"
if [ "$ingest_resp" != "200" ]; then
    echo "Error when requesting to ingesting file by the use of file id, expected 200 got: $ingest_resp"
    exit 1
fi
# Check that the file is ingested and verified
RETRY_TIMES=0
until [ "$(psql -U postgres -h postgres -d sda -At -c "SELECT event FROM sda.file_event_log WHERE file_id='$new_fileid' order by started_at desc limit 1;")" = "verified" ]; do
   echo "waiting for verified to complete"
   RETRY_TIMES=$((RETRY_TIMES + 1))
   if [ "$RETRY_TIMES" -eq 10 ]; then
      echo "::error::Time out while waiting for verified to complete"
      exit 1
   fi
   sleep 2
done
echo "Ingestion by using file ID finished successfully"

# Test giving accession id to a file by using file id
echo "Giving accession id by using file id"
# The file which ingested above will be used
accession_resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $admin_token" -X POST "http://api:8080/file/accession?fileid=$new_fileid&accessionid=SDA-123-asd")"
if [ "$accession_resp" != "200" ]; then
    echo "Error when requesting to finalize file by the use of file id, expected 200 got: $accession_resp"
    exit 1
fi
# Check that the file has been finalized
RETRY_TIMES=0
until [ "$(psql -U postgres -h postgres -d sda -At -c "SELECT event FROM sda.file_event_log WHERE file_id='$new_fileid' order by started_at desc limit 1;")" = "ready" ]; do
   echo "waiting for finalize to complete"
   RETRY_TIMES=$((RETRY_TIMES + 1))
   if [ "$RETRY_TIMES" -eq 10 ]; then
      echo "::error::Time out while waiting for finalizing to complete"
      exit 1
   fi
   sleep 2
done
echo "Finalize by using file ID finished successfully"

filesInDataset="$(curl -s -k -L -H "Authorization: Bearer $token" "http://api:8080/dataset/SYNC-001-12345" | jq -r '.numberOfFiles')"
if [ "$filesInDataset" -ne 2 ]; then
	echo "Error when getting SYNC-001-12345 dataset, expected it to have 2 files, got: $filesInDataset"
	exit 1
fi

dataset_file_removal_resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -X DELETE "http://api:8080/dataset/SYNC-001-12345/files/SYNC-123-00003")"
if [ "$dataset_file_removal_resp" != "202" ]; then
    echo "Error when requesting to remove file from dataset, expected 202 got: $dataset_file_removal_resp"
    exit 1
fi

echo "waiting for dataset file removal to complete"
RETRY_TIMES=0
until [ "$(curl -s -k -L -H "Authorization: Bearer $token" "http://api:8080/dataset/SYNC-001-12345" | jq -r '.numberOfFiles')" -eq 1 ]; do
    echo "waiting for dataset file removal to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for dataset file removal to complete"
        exit 1
    fi
    sleep 2
done

datasetStatus=$(curl -s -k -L -H "Authorization: Bearer $token" "http://api:8080/dataset/SYNC-001-12345" | jq -r '.status')
if [ "$datasetStatus" != "file_removed" ]; then
  echo "unexpected dataset status after file removal, expected it to have \"file_removed\" status, got: $datasetStatus"
  exit 1
fi

# Release dataset again after file removal

dataset_release_resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -X POST "http://api:8080/dataset/release/SYNC-001-12345")"
if [ "$dataset_release_resp" != "200" ]; then
    echo "Error when requesting to release dataset after file removal, expected 200 got: $dataset_release_resp"
    exit 1
fi

echo "waiting for dataset to be released after file removal"
RETRY_TIMES=0
until [ "$(curl -s -k -L -H "Authorization: Bearer $token" "http://api:8080/dataset/SYNC-001-12345" | jq -r '.status')" == "released" ]; do
    echo "waiting for dataset to be released after file removal"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for dataset to be released after file removal"
        exit 1
    fi
    sleep 2
done

# Now deprecate the dataset and then trigger file removal and check that dataset is still deprecated
curl -s -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$(jq -c -n \
                --arg vhost test \
                --arg name sda \
                --argjson properties "$(jq -c -n \
                                              --argjson delivery_mode 2 \
                                              --arg content_encoding UTF-8 \
                                              --arg content_type application/json \
                                              '$ARGS.named'
                                      )" \
                --arg routing_key "mappings" \
                --arg payload "$(jq -r -c -n \
                                       --arg type deprecate \
                                       --arg dataset_id SYNC-001-12345 \
                                       '$ARGS.named')" \
                --arg payload_encoding string \
                '$ARGS.named')" | jq

RETRY_TIMES=0
until [ "$(curl -s -k -L -H "Authorization: Bearer $token" "http://api:8080/dataset/SYNC-001-12345" | jq -r '.status')" == "deprecated" ]; do
    echo "waiting for dataset to be deprecated"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for dataset to be deprecated"
        exit 1
    fi
    sleep 2
done

dataset_file_removal_resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -X DELETE "http://api:8080/dataset/SYNC-001-12345/files/SYNC-123-00004")"
if [ "$dataset_file_removal_resp" != "202" ]; then
    echo "Error when requesting to remove file from dataset after dataset deprecated, expected 202 got: $dataset_file_removal_resp"
    exit 1
fi

echo "waiting for dataset file removal to complete after dataset deprecated"
RETRY_TIMES=0
until [ "$(curl -s -k -L -H "Authorization: Bearer $token" "http://api:8080/dataset/SYNC-001-12345" | jq -r '.numberOfFiles')" -eq 0 ]; do
    echo "waiting for dataset file removal to complete after dataset deprecated"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for dataset file removal to complete after dataset deprecated"
        exit 1
    fi
    sleep 2
done

# Check that dataset is still deprecated after file removal
datasetStatus=$(curl -s -k -L -H "Authorization: Bearer $token" "http://api:8080/dataset/SYNC-001-12345" | jq -r '.status')
if [ "$datasetStatus" != "deprecated" ]; then
  echo "unexpected dataset status after file removal on deprecated dataset, expected it to have 'deprecated' status, got: $datasetStatus"
  exit 1
fi



echo "API admin tests completed successfully"
