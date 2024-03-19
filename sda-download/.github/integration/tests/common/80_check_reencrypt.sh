#!/bin/bash

if [ "$STORAGETYPE" = s3notls ]; then
    exit 0
fi

cd dev_utils || exit 1

# get a token, set up variables
token=$(curl --cacert certs/ca.pem "https://localhost:8000/tokens" | jq -r  '.[0]')

if [ -z "$token" ]; then
    echo "Failed to obtain token"
    exit 1
fi

dataset="https://doi.example/ty009.sfrrss/600.45asasga"
file="dummy_data"
expected_size=1048605

# download decrypted full file,  check file size
curl --cacert certs/ca.pem -H "Authorization: Bearer $token" "https://localhost:8443/s3/$dataset/$file" --output full1.bam

if [ ! -f "full1.bam" ]; then
    echo "Failed to download full1.bam"
    exit 1
fi

file_size=$(stat -c %s full1.bam)  # Get the size of the file

if [ "$file_size" -ne "$expected_size" ]; then
    echo "Incorrect file size for full decrypted file"
    exit 1
fi

export C4GH_PASSPHRASE=strongpass # passphrase for the client crypt4gh key

# test reencrypt the file header with the client public key 
clientkey=$(base64 -w0 client.pub.pem)
reencryptedFile=reencrypted.bam.c4gh
curl --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "Client-Public-Key: $clientkey" "https://localhost:8443/s3-encrypted/$dataset/$file" --output $reencryptedFile
if [ ! -f "$reencryptedFile" ]; then
    echo "Failed to reencrypt the header of the file from sda-download"
    exit 1
fi

file_size_rec=$(stat -c %s $reencryptedFile)
echo "Size of $reencryptedFile: $file_size_rec"

# descrypt the reencrypted file
if ! crypt4gh decrypt --sk client.sec.pem < $reencryptedFile > full2.bam; then
    echo "Failed to descrypt the reencrypted BAM file with the client public key"
    exit 1
fi

if ! cmp --silent full1.bam full2.bam; then
    echo "Decrypted version of the reencrypted file and the original unencrypted file, are different"
    exit 1
fi

# Clean up
rm full1.bam reencrypted.bam.c4gh full2.bam