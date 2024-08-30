#!/bin/bash

cd shared || true

if [ "$STORAGETYPE" = "posix" ]; then
    exit 0
fi

publickey="LS0tLS1CRUdJTiBDUllQVDRHSCBQVUJMSUMgS0VZLS0tLS0Kejd2K1Iya3BPcXVCT2JpWDVOcWVyU1ByZ1ROM0xaME10NDhKaDgzVWprcz0KLS0tLS1FTkQgQ1JZUFQ0R0ggUFVCTElDIEtFWS0tLS0tCg=="
privatekey="LS0tLS1CRUdJTiBDUllQVDRHSCBFTkNSWVBURUQgUFJJVkFURSBLRVktLS0tLQpZelJuYUMxMk1RQUdjMk55ZVhCMEFCUUFBQUFBU2hMeXR5YzEyTU14Ky9ETExsTEpCd0FSWTJoaFkyaGhNakJmCmNHOXNlVEV6TURVQVBOZkNkdHBFb01SVldnVm43cGFGbUJSdzBYUGkyMWtpWVRXa0thZnVaaXJYenhVOEJ1VWwKQzRNYlZvNUZxU1lyd3pxd3NDVUpqWUxObHBtMUhBPT0KLS0tLS1FTkQgQ1JZUFQ0R0ggRU5DUllQVEVEIFBSSVZBVEUgS0VZLS0tLS0K"

payload=$(
    jq -c -n  \
        --arg oldheader "" \
        --arg publickey "$publickey" \
        '$ARGS.named'
)

noHeader=$(/shared/grpcurl -plaintext -d "$payload" reencrypt:50051 reencrypt.Reencrypt.ReencryptHeader 2>&1 | grep "Message:")
if [ "$noHeader" !=  "  Message: no header received" ]; then
    echo "reencrypt without header returned wrong message"
    echo "want: '  Message: no header received', got: $noHeader"
    exit 1
fi

payload=$(
    jq -c -n  \
        --arg oldheader "" \
        --arg publickey "LS0tLS1CRUdJTiBDUllQVDRHSCBQVUJMSUMgS0VZLS0tLS" \
        '$ARGS.named'
)

badPubKey=$(/shared/grpcurl -plaintext -d "$payload" reencrypt:50051 reencrypt.Reencrypt.ReencryptHeader 2>&1 | grep "illegal base64 data")
if [[ ! "$badPubKey" =~  "illegal base64 data" ]]; then
    echo "reencrypt with bad public key returned wrong message"
    echo "want: 'Message: illegal base64 data', got: $badPubKey"
    exit 1
fi


oldHeader=$(head -c 124 NA12878.bam.c4gh | base64 -w0)
payload=$(
    jq -c -n  \
        --arg oldheader "$oldHeader" \
        --arg publickey "$publickey" \
        '$ARGS.named'
)

shouldWork=$(/shared/grpcurl -plaintext -d "$payload" reencrypt:50051 reencrypt.Reencrypt.ReencryptHeader 2>&1 | jq -r '."header"')
if [[ ! "$shouldWork" =~  "Y3J5cHQ0Z2gBAAAAAQAAAGw" ]]; then
    echo "reencrypt failed unexpected"
    echo "response should contain: 'Y3J5cHQ0Z2gBAAAAAQAAAGw', got: $shouldWork"
    exit 1
fi

payload=$(
    jq -c -n  \
        --arg oldheader "$oldHeader" \
        --arg publickey "$publickey" \
        --argjson dataeditlist '["100","500"]' \
        '$ARGS.named'
)

shouldWork=$(/shared/grpcurl -plaintext -d "$payload" reencrypt:50051 reencrypt.Reencrypt.ReencryptHeader 2>&1 | jq -r '."header"')
if [[ ! "$shouldWork" =~  "Y3J5cHQ0Z2gBAAAAAgAAAGw" ]]; then
    echo "reencrypt failed unexpected"
    echo "response should contain: 'Y3J5cHQ0Z2gBAAAAAgAAAGw', got: $shouldWork"
    exit 1
fi

echo "$shouldWork" | base64 -d > reencrypted_file.c4gh
headerSize=$(stat -c '%s' reencrypted_file.c4gh)
# Append the rest of the file
dd if=NA12878.bam.c4gh of=reencrypted_file.c4gh seek=1 obs="$headerSize" skip=1 ibs=124

echo "$privatekey" | base64 -d > private.key

C4GH_PASSPHRASE=password ./crypt4gh decrypt -f reencrypted_file.c4gh -s private.key
fileSize=$(stat -c '%s' reencrypted_file)

if [[ ! "$fileSize" -eq 500 ]]; then
    echo "decrypted reencrypted file has wrong size"
    echo "should be: 500, was: $fileSize"
    exit 1
fi

hash=$(sha256sum reencrypted_file | cut -d ' ' -f 1)
if [[ ! "$hash" =  223da2f47ada1105def2c5da57dde11bb0bb03a15b43a43370336db38e3c441d  ]]; then
    echo "decrypted reencrypted file has wrong hash"
    echo "should be: 223da2f47ada1105def2c5da57dde11bb0bb03a15b43a43370336db38e3c441d, was: $hash"
    exit 1
fi



echo "reencryption test completed successfully"
