#!/bin/bash

if [ "$STORAGETYPE" = s3notls ]; then
    exit 0
fi

cd dev_utils || exit 1

# Get a token, set up variables
token=$(curl --cacert certs/ca.pem "https://localhost:8000/tokens" | jq -r  '.[0]')

if [ -z "$token" ]; then
    echo "Failed to obtain token"
    exit 1
fi

dataset="https://doi.example/ty009.sfrrss/600.45asasga"
file="dummy_data"
expected_size=1048605

# Download unencrypted full file,  check file size
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

# Download encrypted full file, check the file size
encryptedFile=encrypted.bam.c4gh
curl --cacert certs/ca.pem -H "Authorization: Bearer $token" "https://localhost:8443/s3-encrypted/$dataset/$file" --output $encryptedFile
if [ ! -f "$encryptedFile" ]; then
    echo "Failed to download the encrypted file from sda-download"
    exit 1
fi
file_size=$(stat -c %s $encryptedFile)
echo "Size of $encryptedFile: $file_size"

# Descrypt the encrypted file and compare it with the original unencrypted file
export C4GH_PASSPHRASE=$(grep -F passphrase config.yaml | sed -e 's/.* //' -e 's/"//g')
if ! crypt4gh decrypt --sk c4gh.sec.pem  < $encryptedFile > full2.bam; then
    echo "Failed to descrypt the $encryptedFile"
    exit 1
fi

if ! cmp --silent full1.bam full2.bam; then
    echo "Decrypted version of $encryptedFile and the original unencrypted file, are different"
    exit 1
fi

# Test reencrypt the file header with the client public key 
clientkey=$(base64 -w0 client.pub.pem)
reencryptedFile=reencrypted.bam.c4gh
curl --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "Client-Public-Key: $clientkey" "https://localhost:8443/s3-encrypted/$dataset/$file" --output $reencryptedFile
if [ ! -f "$reencryptedFile" ]; then
    echo "Failed to reencrypt the header of the file from sda-download"
    exit 1
fi

file_size=$(stat -c %s $reencryptedFile)
echo "Size of $reencryptedFile: $file_size"

# Descrypt the reencrypted file and compare it with the original unencrypted file
export C4GH_PASSPHRASE="strongpass" # passphrase for the client crypt4gh key
if ! crypt4gh decrypt --sk client.sec.pem < $reencryptedFile > full3.bam; then
    echo "Failed to descrypt $reencryptedFile with the client public key"
    exit 1
fi

if ! cmp --silent full1.bam full3.bam; then
    echo "Decrypted version of $reencryptedFile and the original unencrypted file, are different"
    exit 1
fi

# Clean up
rm full1.bam full2.bam full3.bam $encryptedFile $reencryptedFile