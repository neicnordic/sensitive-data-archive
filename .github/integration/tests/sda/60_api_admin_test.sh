#!/bin/sh
set -e
cd shared || true

token="$(curl http://oidc:8080/tokens | jq -r '.[0]')"
result="$(curl -sk -L "http://api:8080/users/test@dummy.org/files" -H "Authorization: Bearer $token" | jq '. | length')"
if [ "$result" -ne 2 ]; then
    echo "wrong number of files returned for user test@dummy.org"
    echo "expected 4 got $result"
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

## reupload a file under a different name, test to delete it
s3cmd -c s3cfg put NA12878.bam.c4gh s3://test_dummy.org/NC12878.bam.c4gh

echo "waiting for upload to complete"
URI=http://rabbitmq:15672
if [ -n "$PGSSLCERT" ]; then
    URI=https://rabbitmq:15671
fi
RETRY_TIMES=0
until [ "$(curl -s -k -u guest:guest $URI/api/queues/sda/inbox | jq -r '."messages_ready"')" -gt 0 ]; do
    echo "waiting for upload to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 10 ]; then
        echo "::error::Time out while waiting for upload to complete"
        exit 1
    fi
    echo "read now:" `curl -s -k -u guest:guest $URI/api/queues/sda/inbox | jq -r '."messages_ready"'`
    sleep 2
done

# get the fileId of the new file
fileid="$(curl -k -L -H "Authorization: Bearer $token" -H "Content-Type: application/json" "http://api:8080/users/test@dummy.org/files" | jq -r '.[] | select(.inboxPath == "test_dummy.org/NC12878.bam.c4gh") | .fileID')"
echo "Found uploaded file " $fileid

# delete it
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X DELETE "http://api:8080/file/test@dummy.org/$fileid")"
if [ "$resp" != "200" ]; then
	echo "Error when deleting the file, expected 200 got: $resp"
	exit 1
fi

last_event=$(psql -U postgres -h postgres -d sda -At -c "SELECT event FROM sda.file_event_log WHERE file_id='$fileid' order by started_at desc limit 1;")
echo "last event was" $last_event

if [[ "$last_event" != "disabled" ]]; then
    echo "The file $fileid does not have the expected las event 'disabled', but $last_event."
fi