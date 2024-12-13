#!/bin/bash
set -e
cd shared || true

token="$(cat /shared/token)"
# Upload a file and make sure it's listed
result="$(curl -sk -L "http://api:8080/users/test@dummy.org/files" -H "Authorization: Bearer $token" | jq '. | length')"
if [ "$result" -ne 2 ]; then
    echo "wrong number of files returned for user test@dummy.org"
    echo "expected 2 got $result"
    exit 1
fi

## trigger re-verification of EGAF74900000001
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -X PUT "http://api:8080/file/verify/EGAF74900000001")"
if [ "$resp" != "200" ]; then
	echo "Error when starting re-verification, expected 200 got: $resp"
	exit 1
fi

## failure on wrong accession ID
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -X PUT "http://api:8080/file/verify/EGAF74900054321")"
if [ "$resp" != "404" ]; then
	echo "Error when starting re-verification, expected 404 got: $resp"
	exit 1
fi

## trigger re-verification of dataset SYNC-001-12345
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -X PUT "http://api:8080/dataset/verify/SYNC-001-12345")"
if [ "$resp" != "200" ]; then
	echo "Error when starting re-verification of dataset, expected 200 got: $resp"
	exit 1
fi

## expect failure of missing dataset
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -X PUT "http://api:8080/dataset/verify/SYNC-999-12345")"
if [ "$resp" != "404" ]; then
	echo "Error when starting re-verification of missing dataset, expected 404 got: $resp"
	exit 1
fi

# Reupload a file under a different name, test to delete it
s3cmd -c s3cfg put "NA12878.bam.c4gh" s3://test_dummy.org/NC12878.bam.c4gh
stream_size=$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/error_stream | jq '.messages_ready')

echo "waiting for upload to complete"
URI=http://rabbitmq:15672
if [ -n "$PGSSLCERT" ]; then
   URI=https://rabbitmq:15671
fi
RETRY_TIMES=0

sleep 10

if [ $((stream_size++)) -eq "$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/inbox | jq '.messages_ready')" ]; then
   echo "waiting for upload to complete"
   echo "but too bad will eexit"
   exit 1
fi

# get the fileId of the new file
fileid="$(curl -k -L -H "Authorization: Bearer $token" "http://api:8080/users/test@dummy.org/files" | jq -r '.[] | select(.inboxPath == "NC12878.bam.c4gh") | .fileID')"

output=$(s3cmd -c s3cfg ls s3://test_dummy.org/NC12878.bam.c4gh 2>/dev/null)
if [ -z "$output" ] ; then
    echo "Uploaded file not in inbox"
    exit 1
fi
# delete it
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -X DELETE "http://api:8080/file/test@dummy.org/$fileid")"
if [ "$resp" != "200" ]; then
    echo "Error when deleting the file, expected 200 got: $resp"
    exit 1
fi

last_event=$(psql -U postgres -h postgres -d sda -At -c "SELECT event FROM sda.file_event_log WHERE file_id='$fileid' order by started_at desc limit 1;")

if [ "$last_event" != "disabled" ]; then
   echo "The file $fileid does not have the expected las event 'disabled', but $last_event."
fi

output=$(s3cmd -c s3cfg ls s3://test_dummy.org/NC12878.bam.c4gh 2>/dev/null)
if [ -n "$output" ] ; then
    echo "Deleted file is still in inbox"
    exit 1
fi

# Try to delete an unknown file
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -X DELETE "http://api:8080/file/test@dummy.org/badfileid")"
if [ "$resp" != "404" ]; then
    echo "Error when deleting the file, expected error got: $resp"
    exit 1
fi


# Try to delete file of other user
fileid="$(curl -k -L -H "Authorization: Bearer $token" "http://api:8080/users/requester@demo.org/files" | jq -r '.[0]| .fileID')"
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -X DELETE "http://api:8080/file/test@dummy.org/$fileid")"
if [ "$resp" != "404" ]; then
    echo "Error when deleting the file, expected 404 got: $resp"
    exit 1
fi


# Re-upload the file and use the api to ingest it, then try to delete it
s3cmd -c s3cfg put NA12878.bam.c4gh s3://test_dummy.org/NE12878.bam.c4gh

URI=http://rabbitmq:15672
if [ -n "$PGSSLCERT" ]; then
   URI=https://rabbitmq:15671
fi
sleep 10

if [ $((stream_size++)) -eq "$(curl -s -u guest:guest $URI/api/queues/sda/inbox | jq '.messages_ready')" ]; then
   echo "waiting for upload to complete"
   echo "sad"
   exit 1
fi

# Ingest it
new_payload=$(
jq -c -n \
	--arg filepath "NE12878.bam.c4gh" \
	--arg user "test@dummy.org" \
	'$ARGS.named'
)

resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d "$new_payload" "http://api:8080/file/ingest")"
if [ "$resp" != "200" ]; then
    echo "Error when requesting to ingesting file, expected 200 got: $resp"
    exit 1
fi

fileid="$(curl -k -L -H "Authorization: Bearer $token" "http://api:8080/users/test@dummy.org/files" | jq -r '.[] | select(.inboxPath == "NE12878.bam.c4gh") | .fileID')"
# wait for the fail to get the correct status
RETRY_TIMES=0

until [ "$(psql -U postgres -h postgres -d sda -At -c "select id from sda.file_events e where e.title in (select event from sda.file_event_log where file_id = '$fileid' order by started_at desc limit 1);")"  -gt 30 ]; do
   echo "waiting for ingest to complete"
   RETRY_TIMES=$((RETRY_TIMES + 1))
   if [ "$RETRY_TIMES" -eq 30 ]; then
      echo "::error::Time out while waiting for ingest to complete"
      exit 1
   fi
   sleep 2
done

# Try to delete file not in inbox
fileid="$(curl -k -L -H "Authorization: Bearer $token" "http://api:8080/users/test@dummy.org/files" | jq -r '.[] | select(.inboxPath == "NE12878.bam.c4gh") | .fileID')"
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -X DELETE "http://api:8080/file/test@dummy.org/$fileid")"
if [ "$resp" != "404" ]; then
	echo "Error when deleting the file, expected 404 got: $resp"
	exit 1
fi

# Test downloading the file from the inbox
# Reupload a file under a different name, test to download it
s3cmd -c s3cfg put "NA12878.bam.c4gh" s3://test_dummy.org/NC12878.bam.c4gh
fileid="$(curl -k -L -H "Authorization: Bearer $token" "http://api:8080/users/test@dummy.org/files" | jq -r '.[] | select(.inboxPath == "NC12878.bam.c4gh") | .fileID')"
output=$(s3cmd -c s3cfg ls s3://test_dummy.org/NC12878.bam.c4gh 2>/dev/null)
if [ -z "$output" ] ; then
    echo "Uploaded file NC12878.bam.c4gh not found in inbox"
    exit 1
fi

# download the file, re-encrypted with the client key
clientPubKey="$(base64 -w0 /shared/client.pub.pem)"
resp="$(curl -s -k -L -w "%{http_code}\n" -H "Authorization: Bearer $token" -H "C4GH-Public-Key: $clientPubKey" "http://api:8080/users/test@dummy.org/file/$fileid" -JO)"
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
token_nonAdmin=$(grep 'access_token' /shared/s3cfg | sed -E 's/access_token="?([^"]+)"?/\1/')
resp="$(curl -s -k -L -w "%{http_code}\n" -H "Authorization: Bearer $token_nonAdmin" -H "C4GH-Public-Key: $clientPubKey" "http://api:8080/users/test@dummy.org/file/$fileid" -JO)"
if [ "$resp" != "401" ]; then
    echo "Error when downloading the file, expected 401 got: $resp"
    exit 1
fi

# download a non existing file should fail
badfileid="badfileid"
resp="$(curl -s -k -L -w "%{http_code}\n" -H "Authorization: Bearer $token" -H "C4GH-Public-Key: $clientPubKey" "http://api:8080/users/test@dummy.org/file/$badfileid" -JO)"
if [ "$resp" != "404" ]; then
    echo "Trying to download a non existing file, expected 404 got: $resp"
    exit 1
fi
