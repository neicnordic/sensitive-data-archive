#!/bin/bash

if [ "$STORAGETYPE" = s3notls ]; then
    exit 0
fi

cd dev_utils || exit 1

# get a token, set up variables
token=$(curl -s --cacert certs/ca.pem "https://localhost:8000/tokens" | jq -r  '.[0]')
dataset="https://doi.example/ty009.sfrrss/600.45asasga"
file="dummy_data"
expected_size=1048605
C4GH_PASSPHRASE=$(yq .c4gh.passphrase config.yaml)
export C4GH_PASSPHRASE

# download decrypted full file,  check file size
curl -s --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.3.0" "https://localhost:9443/s3/$dataset/$file" --output full1.bam
file_size=$(stat -c %s full1.bam)  # Get the size of the file

if [ "$file_size" -ne "$expected_size" ]; then
    echo "Incorrect file size for full decrypted file"
    exit 1
fi

# test that start, end=0 returns the whole file
curl -s --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.3.0" "https://localhost:9443/s3/$dataset/$file?startCoordinate=0&endCoordinate=0" --output full2.bam

if ! cmp --silent full1.bam full2.bam; then
    echo "Full decrypted files, with and without coordinates, are different"
    exit 1
fi

# cleanup
rm full*.bam

echo "OK"