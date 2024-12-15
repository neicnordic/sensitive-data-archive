#!/bin/sh

for c in s3cmd jq
do
    if ! command -v $c
    then
        echo "$c could not be found"
        exit 1
    fi
done

cat << EOF > c4gh.pub.pem
-----BEGIN CRYPT4GH PUBLIC KEY-----
avFAerx0ZWuJE6fTI8S/0wv3yMo1n3SuNTV6zvKdxQc=
-----END CRYPT4GH PUBLIC KEY-----
EOF

chmod 444 c4gh.pub.pem

cat << EOF > c4gh.sec.pem
-----BEGIN CRYPT4GH ENCRYPTED PRIVATE KEY-----
YzRnaC12MQAGc2NyeXB0ABQAAAAAwAs5mVkXda50vqeYv6tbkQARY2hhY2hhMjBf
cG9seTEzMDUAPAd46aTuoVWAe+fMGl3VocCKCCWmgFUsFIHejJoWxNwy62c1L/Vc
R9haQsAPfJMLJSvUXStJ04cyZnDHSw==
-----END CRYPT4GH ENCRYPTED PRIVATE KEY-----
EOF

chmod 444 c4gh.sec.pem


# insert file entry into database
file_id=$(docker run --rm --name client --network dev_utils_default \
    neicnordic/pg-client:latest \
    postgresql://postgres:rootpassword@db:5432/lega \
    -t -q -c "INSERT INTO sda.files (stable_id, submission_user, \
        submission_file_path, submission_file_size, archive_file_path, \
        archive_file_size, decrypted_file_size, backup_path, header, \
        encryption_method) VALUES ('urn:neic:001-002', 'integration-test', 'dummy_data.c4gh', \
        1048729, '4293c9a7-dc50-46db-b79a-27ddc0dad1c6', 1049081, 1048605, \
        '', '637279707434676801000000010000006c000000000000006af1407abc74656b8913a7d323c4bfd30bf7c8ca359f74ae35357acef29dc5073799e207ec5d022b2601340585ff082565e55fbff5b6cdbbbe6b12a0d0a19ef325a219f8b62344325e22c8d26a8e82e45f053f4dcee10c0ec4bb9e466d5253f139dcd4be', 'CRYPT4GH') RETURNING id;" | xargs)

# insert "ready" database log event
docker run --rm --name client --network dev_utils_default \
    neicnordic/pg-client:latest \
    postgresql://postgres:rootpassword@db:5432/lega \
    -t -q -c "INSERT INTO sda.file_event_log (file_id, event) \
        VALUES ('$file_id', 'ready');"

docker run --rm --name client --network dev_utils_default \
	neicnordic/pg-client:latest \
    postgresql://postgres:rootpassword@db:5432/lega \
    -t -q -c "INSERT INTO sda.checksums (file_id, checksum, type, source) \
        VALUES ('$file_id', '06bb0a514b26497b4b41b30c547ad51d059d57fb7523eb3763cfc82fdb4d8fb7', 'SHA256', 'UNENCRYPTED');"

docker run --rm --name client --network dev_utils_default \
	neicnordic/pg-client:latest \
    postgresql://postgres:rootpassword@db:5432/lega \
    -t -q -c "INSERT INTO sda.checksums (file_id, checksum, type, source) \
        VALUES ('$file_id', '5e9c767958cc3f6e8d16512b8b8dcab855ad1e04e05798b86f50ef600e137578', 'SHA256', 'UPLOADED');"

docker run --rm --name client --network dev_utils_default \
	neicnordic/pg-client:latest \
    postgresql://postgres:rootpassword@db:5432/lega \
    -t -q -c "INSERT INTO sda.checksums (file_id, checksum, type, source) \
        VALUES ('$file_id', '74820dbcf9d30f8ccd1ea59c17d5ec8a714aabc065ae04e46ad82fcf300a731e', 'SHA256', 'ARCHIVED');"


# make sure that the dataset exists in the database
dataset_id=$(docker run --rm --name client --network dev_utils_default \
    neicnordic/pg-client:latest \
    postgresql://postgres:rootpassword@db:5432/lega \
    -t -q -c "INSERT INTO sda.datasets (stable_id) VALUES ('https://doi.example/ty009.sfrrss/600.45asasga') \
        ON CONFLICT (stable_id) DO UPDATE \
        SET stable_id=excluded.stable_id RETURNING id;")

# insert the file into the dataset
docker run --rm --name client --network dev_utils_default \
    neicnordic/pg-client:latest \
    postgresql://postgres:rootpassword@db:5432/lega \
    -t -q -c "INSERT INTO sda.file_dataset (file_id, dataset_id) \
        VALUES ('$file_id', $dataset_id);"

# Make buckets if they don't exist already
s3cmd -c s3cmd-notls.conf mb s3://archive || true

# Upload test file
s3cmd -c s3cmd-notls.conf put archive_data/4293c9a7-dc50-46db-b79a-27ddc0dad1c6 s3://archive/4293c9a7-dc50-46db-b79a-27ddc0dad1c6



# Test Health Endpoint
check_health=$(curl -o /dev/null -s -w "%{http_code}\n" http://localhost:8080/health)

if [ "$check_health" != "200" ]; then
    echo "Health endpoint does not respond properly"
    echo "got: ${check_health}"
    exit 1
fi

echo "Health endpoint is ok"

# Test empty token

check_401=$(curl -o /dev/null -s -w "%{http_code}\n" -X GET http://localhost:8080/metadata/datasets)

if [ "$check_401" != "401" ]; then
    echo "no token provided should give 401"
    echo "got: ${check_401}"
    exit 1
fi

echo "got correct response when no token provided"

check_405=$(curl -X POST -o /dev/null -s -w "%{http_code}\n" http://localhost:8080/metadata/datasets )

if [ "$check_405" != "405" ]; then
    echo "POST should not be allowed"
    echo "got: ${check_405}"
    exit 1
fi

echo "got correct response when POST method used"

# Test good token

token=$(curl "http://localhost:8000/tokens" | jq -r  '.[0]')

## Test datasets endpoint

check_dataset=$(curl -H "Authorization: Bearer $token" http://localhost:8080/metadata/datasets | jq -r '.[0]')

if [ "$check_dataset" != "https://doi.example/ty009.sfrrss/600.45asasga" ]; then
    echo "dataset https://doi.example/ty009.sfrrss/600.45asasga not found"
    echo "got: ${check_dataset}"
    exit 1
fi

echo "expected dataset found"

## Test datasets/files endpoint

check_files=$(curl -H "Authorization: Bearer $token" "http://localhost:8080/metadata/datasets/https://doi.example/ty009.sfrrss/600.45asasga/files" | jq -r '.[0].fileId')

if [ "$check_files" != "urn:neic:001-002" ]; then
    echo "file with id urn:neic:001-002 not found"
    echo "got: ${check_files}"
    exit 1
fi

echo "expected file found"

# Test file can be decrypted
## test also the files endpoint

C4GH_PASSPHRASE=$(grep -F passphrase config.yaml | tail -1 | sed -e 's/.* //' -e 's/"//g')
export C4GH_PASSPHRASE

crypt4gh decrypt --sk c4gh.sec.pem < dummy_data.c4gh > old-file.txt

curl -H "Authorization: Bearer $token" "http://localhost:8080/files/urn:neic:001-002" --output test-download.txt

cmp --silent old-file.txt test-download.txt
status=$?
if [ $status = 0 ]; then
    echo "Files are the same"
else
    echo "Files are different"
fi

# Test get visas failed

token=$(curl "http://localhost:8000/tokens" | jq -r  '.[1]')

## Test datasets endpoint

check_empty_token=$(curl -o /dev/null -s -w "%{http_code}\n" -H "Authorization: Bearer $token" http://localhost:8080/metadata/datasets)

if [ "$check_empty_token" != "200" ]; then
    echo "response for empty token is not 200"
    echo "got: ${check_empty_token}"
    exit 1
fi

echo "got correct response when token has no permissions"
