#!/bin/bash

if [ "$STORAGETYPE" = s3notls ]; then
    exit 0
fi

cd dev_utils || exit 1

# get a token, set up variables
token=$(curl --cacert certs/ca.pem "https://localhost:8000/tokens" | jq -r  '.[0]')
dataset="https://doi.example/ty009.sfrrss/600.45asasga"
file="dummy_data"
expected_size=1048605
C4GH_PASSPHRASE=$(grep -F passphrase config.yaml | sed -e 's/.* //' -e 's/"//g')
export C4GH_PASSPHRASE

# download decrypted full file,  check file size
curl --cacert certs/ca.pem -H "Authorization: Bearer $token" "https://localhost:8443/s3/$dataset/$file" --output full1.bam
file_size=$(stat -c %s full1.bam)  # Get the size of the file

if [ "$file_size" -ne "$expected_size" ]; then
    echo "Incorrect file size for full decrypted file"
    exit 1
fi

# test that start, end=0 returns the whole file
curl --cacert certs/ca.pem -H "Authorization: Bearer $token" "https://localhost:8443/s3/$dataset/$file?startCoordinate=0&endCoordinate=0" --output full2.bam

cmp --silent full1.bam full2.bam
status=$?
if [[ $status != 0 ]]; then
    echo "Full decrypted files, with and without coordinates, are different"
    exit 1
fi

# download encrypted full file, check that it can be decrypted correctly 
curl --cacert certs/ca.pem -H "Authorization: Bearer $token" "https://localhost:8443/s3-encrypted/$dataset/$file" --output full3.bam.c4gh
full_file_size=$(stat -c %s full3.bam.c4gh)
expected_encrypted_size=1049205

if [ "$full_file_size" -ne "$expected_encrypted_size" ]; then
    echo "Incorrect file size for full encrypted file"
    exit 1
fi

crypt4gh decrypt --sk c4gh.sec.pem < full3.bam.c4gh > full3.bam
cmp --silent full1.bam full3.bam
status=$?
if [[ $status != 0 ]]; then
    echo "Full encrypted files is not correct when decrypting"
    exit 1
fi

# download full encrypted file, test that start, end=0 returns the whole file
curl --cacert certs/ca.pem -H "Authorization: Bearer $token" "https://localhost:8443/s3-encrypted/$dataset/$file?startCoordinate=0&endCoordinate=0" --output full4.bam.c4gh

cmp --silent full3.bam.c4gh full4.bam.c4gh
status=$?
if [[ $status != 0 ]]; then
    echo "Full encrypted files with coordinates is not correct"
    exit 1
fi

# download encrypted partial file, check file size
curl --cacert certs/ca.pem -H "Authorization: Bearer $token" "https://localhost:8443/s3-encrypted/$dataset/$file?startCoordinate=0&endCoordinate=1000" --output part1.bam.c4gh
file_size=$(stat -c %s part1.bam.c4gh)  # Get the size of the file
part_expected_size=65688

if [ "$file_size" -ne "$part_expected_size" ]; then
    echo "Incorrect file size for partial encrypted file"
    exit 1
fi

crypt4gh decrypt --sk c4gh.sec.pem < part1.bam.c4gh > part1.bam
status=$?
if [[ $status != 0 ]]; then
    echo "Partial encrypted file could not be decrypted"
    exit 1
fi


part_decrypted_size=65536
file_size=$(stat -c %s part1.bam)
if [ "$file_size" -ne "$part_decrypted_size" ]; then
    echo "Incorrect file size for partial decrypted file"
    exit 1
fi

if ! grep -q "^THIS FILE IS JUST DUMMY DATA" part1.bam; then
    echo "Bad content partial decrypted file"
    exit 1
fi

# download partial encrypted file using http headers
stopCoord=200
curl --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "Range: bytes=0-$stopCoord" "https://localhost:8443/s3-encrypted/$dataset/$file" --output part2.bam.c4gh
file_size=$(stat -c %s part2.bam.c4gh)

if [ "$file_size" -ne "$stopCoord" ]; then
    echo "Incorrect file size for partial encrypted file, using Range"
    exit 1
fi
