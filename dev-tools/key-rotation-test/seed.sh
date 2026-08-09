#!/bin/sh
set -e

# This script is used to seed the database with test data for the key rotation test.

# Helper function to fast-poll until the file is verified and decrypted by background workers
wait_to_ready() {
    _FILE_UUID=$1
    echo "Waiting for background workers to verify file $_FILE_UUID..."

    # Poll up to 50 times with a 0.1-second sleep (total 5 seconds max)
    for i in $(seq 1 50); do
        # Verify the current event milestone on the file
        LAST_EVENT=$(PGPASSWORD=rootpasswd psql -U postgres -h postgres -d sda -At -c \
            "SELECT last_event FROM sda.files WHERE id='$_FILE_UUID';" 2>/dev/null | tr -d '\r\n')

        # Verify the unencrypted verification checksum is registered
        HAS_CHECKSUM=$(PGPASSWORD=rootpasswd psql -U postgres -h postgres -d sda -At -c \
            "SELECT 1 FROM sda.checksums WHERE file_id='$_FILE_UUID' AND source='UNENCRYPTED' LIMIT 1;" 2>/dev/null | tr -d '\r\n')

        if [ "$LAST_EVENT" = "verified" ] || [ "$LAST_EVENT" = "ready" ]; then
            if [ "$HAS_CHECKSUM" = "1" ]; then
                return 0
            fi
        fi
        sleep 0.1
    done

    echo "❌ Error: Timeout waiting for file $_FILE_UUID to finish background verification."
    exit 1
}

echo "=== Starting Automated Multi-File S3cmd & Admin-API Ingestion Pipeline ==="

# Define core configuration variables
API_URL="http://api:8080"
USER_ID="test@dummy.org"
BUCKET_TARGET="test_dummy.org"
DATASET_FOLDER="dataset_folder"
DATASET_ID="EGAD00000000001"

# Arrays to keep track of the files we are processing
FILES="file1.txt file2.txt file3.txt file4.txt"
ACCESSION_IDS=""

# 1. Fetch the pre-generated integration token
if [ ! -f "/shared/token" ]; then
    echo "❌ Error: Token file not found at /shared/token. Ensure credentials container runs first."
    exit 1
fi
TOKEN=$(cat /shared/token | tr -d '\n')

# Install dependencies inside runtime environment
apt-get -o DPkg::Lock::Timeout=60 update > /dev/null
apt-get -o DPkg::Lock::Timeout=60 install -y s3cmd curl jq postgresql-client > /dev/null

# Clean and prepare a temporary storage playground
mkdir -p /tmp/test-data
rm -f /tmp/test-data/*


# Process, encrypt, and upload each file individually
COUNTER=101
for filename in $FILES; do
    echo "--------------------------------------------------"
    echo "Processing local file: $filename"
    
    # Write unique dummy content inside each file
    echo "Confidential genomic sequencing data block for $filename" > "/tmp/test-data/$filename"
    
    yes | /shared/crypt4gh encrypt \
      -p /shared/c4gh.pub.pem \
      -f "/tmp/test-data/$filename"
      
    # Construct exact target bucket structural destination path
    S3_TARGET_URI="s3://$BUCKET_TARGET/$DATASET_FOLDER/$filename.c4gh"
    API_INBOX_PATH="$DATASET_FOLDER/$filename.c4gh"
    
    echo "Uploading via s3cmd to: $S3_TARGET_URI"
    s3cmd -c /shared/s3cfg put "/tmp/test-data/$filename.c4gh" "$S3_TARGET_URI"

    # Extract file UUID
    FILE_UUID=""
    for i in $(seq 1 10); do
        FILE_UUID=$(PGPASSWORD=rootpasswd psql -U postgres -h postgres -d sda -At -c \
            "SELECT id FROM sda.files WHERE submission_file_path='$API_INBOX_PATH' ORDER BY created_at DESC LIMIT 1;")
        FILE_UUID=$(echo "$FILE_UUID" | tr -d '\r\n[:space:]')
        if [ -n "$FILE_UUID" ]; then
            break
        fi
        sleep 1
    done

    if [ -z "$FILE_UUID" ]; then
        echo "❌ Error: Unable to retrieve UUID for $filename.c4gh after 10 attempts."
        exit 1
    fi
    
    echo "Triggering ingestion for $filename.c4gh..."
    curl -fsS -H "Authorization: Bearer $TOKEN" -X POST "$API_URL/file/ingest?fileid=$FILE_UUID"

    # Wait for background workers to verify and decrypt the file
    wait_to_ready "$FILE_UUID"
    
    RAND_SUFFIX=$(printf "%011d" $COUNTER)
    FILE_ACCESSION_ID="EGAF${RAND_SUFFIX}"
    echo "Assigning Accession ID: $FILE_ACCESSION_ID to UUID: $FILE_UUID"
    
    curl -fsS -H "Authorization: Bearer $TOKEN" -X POST "$API_URL/file/accession?fileid=$FILE_UUID&accessionid=$FILE_ACCESSION_ID"
         
    # Track assigned accession references in our runner loop string space for dataset bundling
    if [ -z "$ACCESSION_IDS" ]; then
        ACCESSION_IDS="\"$FILE_ACCESSION_ID\""
    else
        ACCESSION_IDS="$ACCESSION_IDS, \"$FILE_ACCESSION_ID\""
    fi
    COUNTER=$((COUNTER+1))
done

echo "--------------------------------------------------"
echo "All files uploaded. Waiting briefly for messaging brokers to process tasks..."

# Verification Loop Monitoring
# Confirm files reach structural completion stability state across tables
MAX_ATTEMPTS=30
ATTEMPT=1
while [ $ATTEMPT -le $MAX_ATTEMPTS ]; do
    # Fetch user file list status from endpoint
    STATUS_RESP=$(curl -sS -H "Authorization: Bearer $TOKEN" "$API_URL/users/$USER_ID/files?path_prefix=$DATASET_FOLDER")
    
    COMPLETED_COUNT=$(echo "$STATUS_RESP" | jq -r '[.[] | select(.fileStatus == "ready" )] | length')
    
    if [ "$COMPLETED_COUNT" -eq 4 ]; then
        break
    fi
    
    sleep 1
    ATTEMPT=$((ATTEMPT+1))
done

if [ "$COMPLETED_COUNT" -ne 4 ]; then
    echo "❌ Timeout waiting for pipeline background workers to finalize all 4 file segments."
    exit 1
fi

echo "Create dataset $DATASET_ID..."
JSON_PAYLOAD="{\"accession_ids\": [$ACCESSION_IDS], \"dataset_id\": \"$DATASET_ID\", \"user\": \"$USER_ID\"}"

curl -fsS -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -X POST \
     -d "$JSON_PAYLOAD" \
     "$API_URL/dataset/create"

# debug 
echo "=== Debug: Listing contents of S3 bucket after creation ==="
s3cmd -c /shared/s3cfg ls s3://$BUCKET_TARGET/$DATASET_FOLDER/

echo "=== 🎉 Success! Dataset '$DATASET_ID' is initialized with 4 clean targets, structured correctly, and ready for rotation! ==="