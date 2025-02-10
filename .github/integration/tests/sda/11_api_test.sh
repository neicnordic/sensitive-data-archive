#!/bin/sh
set -e

# Test the API files endpoint
token="$(cat /shared/token)"
response="$(curl -s -k -L "http://api:8080/users/test@dummy.org/files" -H "Authorization: Bearer $token" | jq -r 'sort_by(.inboxPath)|.[-1].fileStatus')"
if [ "$response" != "uploaded" ]; then
	echo "API returned incorrect value, expected ready got: $response"
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

ts=$(date +"%F %T")
depr="$(curl -s -k -L -H "Authorization: Bearer $token" -X GET "http://api:8080/c4gh-keys/list" | jq -r .[1].deprecated_at)"
if [ "$depr" != "$ts" ]; then
	echo "Error when listing key hash, expected $ts got: $depr"
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
