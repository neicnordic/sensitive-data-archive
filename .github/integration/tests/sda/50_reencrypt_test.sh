#!/bin/bash

cd shared || true

if [ "$STORAGETYPE" = "posix" ]; then
    exit 0
fi

payload=$(
    jq -c -n  \
        --arg oldheader "" \
        --arg publickey "LS0tLS1CRUdJTiBDUllQVDRHSCBQVUJMSUMgS0VZLS0tLS0Kb1BWeUg0Umd0cmdUcjZFV3RsbjEzcitLdkhnS1FRYXlvUHVtS09xeWpFUT0KLS0tLS1FTkQgQ1JZUFQ0R0ggUFVCTElDIEtFWS0tLS0tCg==" \
        '$ARGS.named'
)

noHeader=$(/shared/grpcurl -plaintext -d "$payload" reencrypt:50051 reencrypt.Reencrypt.ReencryptHeader 2>&1 | grep "Message:")
if [ "$noHeader" !=  "  Message: no header recieved" ]; then
    echo "reencrypt without header returned wrong message"
    echo "want: '  Message: no header recieved', got: $noHeader"
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


echo "reencryption test completed successfully"