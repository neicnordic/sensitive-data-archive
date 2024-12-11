#!/bin/bash

if [ "$STORAGETYPE" = s3notls ]; then
    exit 0
fi

cd dev_utils || exit 1

# Get a token, set up variables
token=$(curl -s --cacert certs/ca.pem "https://localhost:8000/tokens" | jq -r  '.[0]')

if [ -z "$token" ]; then
    echo "Failed to obtain token"
    exit 1
fi

dataset="https://doi.example/ty009.sfrrss/600.45asasga"
file="dummy_data"
expected_size=1048605

# Download unencrypted full file (from download service at port 9443), check file size
curl -s --cacert certs/ca.pem -H "Authorization: Bearer $token" "https://localhost:9443/s3/$dataset/$file" --output full1.bam

file_size=$(stat -c %s full1.bam)  # Get the size of the file
if [ "$file_size" -ne "$expected_size" ]; then
    echo "Incorrect file size for downloaded file"
    exit 1
fi

# Test reencrypt the file header with the client public key 
clientkey=$(base64 -w0 client.pub.pem)
reencryptedFile=reencrypted.bam.c4gh
curl -s --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "Client-Public-Key: $clientkey" "https://localhost:8443/s3/$dataset/$file" --output $reencryptedFile

expected_encrypted_size=1049205
file_size=$(stat -c %s $reencryptedFile)
if [ "$file_size" -ne "$expected_encrypted_size" ]; then
    echo "Incorrect file size for the re-encrypted file, should be $expected_encrypted_size but is $file_size"
    exit 1
fi

# Decrypt the reencrypted file and compare it with the original unencrypted file
export C4GH_PASSPHRASE="strongpass" # passphrase for the client crypt4gh key
crypt4gh decrypt -s client.sec.pem -f $reencryptedFile
if [ ! -f "${reencryptedFile%.c4gh}" ] ; then
    echo "Failed to decrypt re-encrypted file with the client's private key"
    exit 1
fi
mv "${reencryptedFile%.c4gh}" full2.bam


if ! cmp --silent full1.bam full2.bam; then
    echo "Decrypted version of $reencryptedFile and the original unencrypted file, are different"
    exit 1
fi

# download reencrypted partial file, check file size
partReencryptedFile=part1.bam.c4gh
curl -s --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "Client-Public-Key: $clientkey" "https://localhost:8443/s3/$dataset/$file?startCoordinate=0&endCoordinate=1000" --output $partReencryptedFile
file_size=$(stat -c %s $partReencryptedFile)  # Get the size of the file
part_expected_size=65688

if [ "$file_size" -ne "$part_expected_size" ]; then
    echo "Incorrect file size for re-encrypted partial file, should be $part_expected_size but is $file_size"
    exit 1
fi

crypt4gh decrypt -s client.sec.pem -f $partReencryptedFile
if [ ! -f "${partReencryptedFile%.c4gh}" ] ; then
    echo "Re-encrypted partial file could not be decrypted"
    exit 1
fi

part_decrypted_size=65536
file_size=$(stat -c %s part1.bam)
if [ "$file_size" -ne "$part_decrypted_size" ]; then
    echo "Incorrect file size for decrypted partial file, should be $part_decrypted_size but is $file_size"
    exit 1
fi

if ! grep -q "^THIS FILE IS JUST DUMMY DATA" part1.bam; then
    echo "Bad content of decrypted partial file"
    exit 1
fi

# Clean up
rm full1.bam full2.bam part1.bam* $reencryptedFile

echo "OK"