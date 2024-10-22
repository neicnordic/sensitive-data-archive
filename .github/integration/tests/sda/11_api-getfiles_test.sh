#!/bin/sh
set -e

# Test the API files endpoint
token="$(curl http://oidc:8080/tokens | jq -r '.[0]')"
response="$(curl -s -k -L "http://api:8080/files" -H "Authorization: Bearer $token" | jq -r 'sort_by(.inboxPath)|.[-1].fileStatus')"
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

manual_hash=$(sed -n '2p' /shared/c4gh.pub.pem | base64 -d -w0 | xxd -c64 -ps)

db_hash=$(psql -U postgres -h postgres -d sda -At -c "SELECT key_hash FROM sda.encryption_keys WHERE description = 'this is the key description';")
if [ "$db_hash" != "$manual_hash" ]; then
	echo "wrong hash in the database, expected $manual_hash got $db_hash"
	exit 1
fi

echo "api test completed successfully"
