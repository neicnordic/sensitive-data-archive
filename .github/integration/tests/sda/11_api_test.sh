#!/bin/sh
set -e

# Test the API files endpoint
token="$(cat /shared/token)"
response="$(curl -s -k -L "http://api:8080/users/test@dummy.org/files" -H "Authorization: Bearer $token" | jq -r 'sort_by(.inboxPath)|.[-1].fileStatus')"
if [ "$response" != "uploaded" ]; then
	echo "API returned incorrect value, expected ready got: $response"
	exit 1
fi

# Test user-owned inbox file delete via file ID.
# Delete the duplicate file created in upload tests so later ingest tests remain stable.
delete_target_path="NB12878.bam.c4gh"
delete_file_id="$(curl -s -k -L "http://api:8080/users/test@dummy.org/files" -H "Authorization: Bearer $token" | jq -r --arg p "$delete_target_path" '.[] | select(.inboxPath == $p and .fileStatus == "uploaded") | .fileId' | head -n 1)"
if [ -z "$delete_file_id" ] || [ "$delete_file_id" = "null" ]; then
	echo "Failed to find uploaded file ID for delete target: $delete_target_path"
	exit 1
fi

resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -X DELETE "http://api:8080/users/test@dummy.org/file/$delete_file_id")"
if [ "$resp" != "200" ]; then
	echo "Error when deleting user inbox file, expected 200 got: $resp"
	exit 1
fi

# User path segment must match authenticated user.
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -X DELETE "http://api:8080/users/requester@demo.org/file/$delete_file_id")"
if [ "$resp" != "403" ]; then
	echo "Delete with mismatched username should fail, expected 403 got: $resp"
	exit 1
fi

latest_status="$(psql -U postgres -h postgres -d sda -At -c "SELECT event FROM sda.file_event_log WHERE file_id = '$delete_file_id' ORDER BY id DESC LIMIT 1;")"
if [ "$latest_status" != "removed" ]; then
	echo "Delete endpoint should set latest file event to removed, got: $latest_status"
	exit 1
fi

listed_after_delete="$(curl -s -k -L "http://api:8080/users/test@dummy.org/files" -H "Authorization: Bearer $token" | jq -r --arg p "$delete_target_path" '[.[] | select(.inboxPath == $p)] | length')"
if [ "$listed_after_delete" -ne 0 ]; then
	echo "Deleted file should not be listed for user, found: $listed_after_delete"
	exit 1
fi

# test inserting a c4gh public key hash
payload=$(
	jq -c -n \
		--arg description "this is the key description" \
		--arg pubkey "$( base64 -w0 /shared/c4gh.pub.pem)" \
		'$ARGS.named'
)

resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d "$payload" "http://api:8080/c4gh-keys/add")"
if [ "$resp" != "200" ]; then
	echo "Error when adding a public key hash, expected 200 got: $resp"
	exit 1
fi

# again to verify we get an error
resp="$(curl -s -k -L  -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d "$payload" "http://api:8080/c4gh-keys/add")"
if [ "$resp" != "409" ]; then
	echo "Error when adding a public key hash, expected 409 got: $resp"
	exit 1
fi

# add key that will be deprecated
new_payload=$(
	jq -c -n \
		--arg description "this key will be deprecated" \
		--arg pubkey "LS0tLS1CRUdJTiBDUllQVDRHSCBQVUJMSUMgS0VZLS0tLS0KTmdUdEFNLzRIUVR4b0I5bHZlRHVaYW5sRmVpWXVHRzBQTTg1eHNBU2xrZz0KLS0tLS1FTkQgQ1JZUFQ0R0ggUFVCTElDIEtFWS0tLS0tCg==" \
		'$ARGS.named'
)

resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d "$new_payload" "http://api:8080/c4gh-keys/add")"
if [ "$resp" != "200" ]; then
	echo "Error when adding a public key hash, expected 200 got: $resp"
	exit 1
fi

deprecated_hash="3604ed00cff81d04f1a01f65bde0ee65a9e515e898b861b43ccf39c6c0129648"

resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST "http://api:8080/c4gh-keys/deprecate/$deprecated_hash")"
if [ "$resp" != "200" ]; then
	echo "Error when adding a public key hash, expected 200 got: $resp"
	exit 1
fi

# Verify the deprecated key has a deprecated_at timestamp set
depr="$(curl -s -k -L -H "Authorization: Bearer $token" -X GET "http://api:8080/c4gh-keys/list" | jq -r .[1].deprecatedAt)"
if [ -z "$depr" ] || [ "$depr" = "null" ]; then
	echo "Error when listing key hash, deprecatedAt should be set, got: $depr"
	exit 1
fi

# list key hashes
resp="$(curl -s -k -L -H "Authorization: Bearer $token" -X GET "http://api:8080/c4gh-keys/list" | jq '. | length')"
if [ "$resp" -ne 2 ]; then
	echo "Error when listing key hash, expected 2 entries got: $resp"
	exit 1
fi

manual_hash=$(sed -n '2p' /shared/c4gh.pub.pem | base64 -d -w0 | xxd -c64 -ps)
resp="$(curl -s -k -L -H "Authorization: Bearer $token" -X GET "http://api:8080/c4gh-keys/list" | jq -r .[0].hash)"
if [ "$resp" != "$manual_hash" ]; then
	echo "Error when listing key hash, expected $manual_hash got: $resp"
	exit 1
fi

echo "api test completed successfully"
