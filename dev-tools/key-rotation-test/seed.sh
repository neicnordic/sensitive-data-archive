#!/bin/sh
set -e

echo "=== Starting Automated Multi-File S3cmd & Admin-API Ingestion Pipeline ==="

# Define core configuration variables
API_URL="http://api:8080"
USER_ID="test@dummy.org"
BUCKET_TARGET="test_dummy.org"
DATASET_FOLDER="dataset_folder"
DATASET_ID="EGAD00000000001"

# Arrays to keep track of the files we are processing
FILES="file1.txt file2.txt file3.txt"
ACCESSION_IDS=""

# 1. Fetch the pre-generated integration token
if [ ! -f "/shared/token" ]; then
    echo "❌ Error: Token file not found at /shared/token. Ensure credentials container runs first."
    exit 1
fi
TOKEN=$(cat /shared/token | tr -d '\n')

# 2. Install dependencies inside runtime environment
apt-get -o DPkg::Lock::Timeout=60 update > /dev/null
apt-get -o DPkg::Lock::Timeout=60 install -y s3cmd curl jq postgresql-client > /dev/null

# Clean and prepare a temporary storage playground
mkdir -p /tmp/test-data
rm -f /tmp/test-data/*


# 3. Process, encrypt, and upload each file individually
for filename in $FILES; do
    echo "--------------------------------------------------"
    echo "Processing local file: $filename"
    
    # Write unique dummy content inside each file
    echo "Confidential genomic sequencing data block for $filename - 2026" > "/tmp/test-data/$filename"
    
    # Secure payload via crypt4gh
    /shared/crypt4gh encrypt \
      --sk /shared/client.sec.pem \
      --pk /shared/c4gh.pub.pem \
      -p c4ghpass \
      < "/tmp/test-data/$filename" > "/tmp/test-data/$filename.c4gh"
      
    # Construct exact target bucket structural destination path
    # s3cmd targets: s3://inbox/USER_ID/dataset_folder/filename.c4gh
    S3_TARGET_URI="s3://$BUCKET_TARGET/$DATASET_FOLDER/$filename.c4gh"
    API_INBOX_PATH="/$DATASET_FOLDER/$filename.c4gh"
    
    echo "Uploading via s3cmd to: $S3_TARGET_URI"
    s3cmd -c /shared/s3cfg put "/tmp/test-data/$filename.c4gh" "$S3_TARGET_URI"
    
    # 4. Trigger ingestion via Admin API
    echo "Triggering API ingestion entrypoint for $filename.c4gh..."
    curl -fsS -H "Authorization: Bearer $TOKEN" \
         -H "Content-Type: application/json" \
         -X POST \
         -d "{\"filepath\": \"$API_INBOX_PATH\", \"user\": \"$USER_ID\"}" \
         "$API_URL/file/ingest"
         
    # 5. Extract unique database file UUID assigned by the database engine
    FILE_UUID=""
    for i in $(seq 1 10); do
        FILE_UUID=$(PGPASSWORD=rootpasswd psql -U postgres -h postgres -d sda -At -c \
            "SELECT id FROM sda.files WHERE filepath='$API_INBOX_PATH' ORDER BY created_at DESC LIMIT 1;")
        if [ -n "$FILE_UUID" ]; then
            break
        fi
        sleep 1
    done
    
    if [ -z "$FILE_UUID" ]; then
        echo "❌ Error tracing database configuration metadata map for $filename.c4gh"
        exit 1
    fi
    
    # Generate an explicit random Accession Identifier string for the target file instance
    # Example format: EGAF00000000101, EGAF00000000102...
    RAND_SUFFIX=$(od -An -N3 -tu4 /dev/urandom | tr -d ' ')
    FILE_ACCESSION="EGAF${RAND_SUFFIX}"
    echo "Assigning Accession string ID: [$FILE_ACCESSION] to UUID: $FILE_UUID"
    
    # 6. Apply Accession registration metadata via Admin API
    curl -fsS -H "Authorization: Bearer $TOKEN" \
         -H "Content-Type: application/json" \
         -X POST \
         -d "{\"accession_id\": \"$FILE_ACCESSION\", \"filepath\": \"$API_INBOX_PATH\", \"user\": \"$USER_ID\"}" \
         "$API_URL/file/accession"
         
    # Track assigned accession references in our runner loop string space for dataset bundling
    if [ -z "$ACCESSION_IDS" ]; then
        ACCESSION_IDS="\"$FILE_ACCESSION\""
    else
        ACCESSION_IDS="$ACCESSION_IDS, \"$FILE_ACCESSION\""
    fi
done

echo "--------------------------------------------------"
echo "All files uploaded. Waiting briefly for messaging brokers to process tasks..."

# 7. Verification Loop Monitoring
# Confirm files reach structural completion stability state across tables
MAX_ATTEMPTS=30
ATTEMPT=1
while [ $ATTEMPT -le $MAX_ATTEMPTS ]; do
    # Fetch user file list status from endpoint
    STATUS_RESP=$(curl -sS -H "Authorization: Bearer $TOKEN" "$API_URL/files?path_prefix=$USER_ID/$DATASET_FOLDER")
    
    # Parse total completed status elements vs total target count (3)
    COMPLETED_COUNT=$(echo "$STATUS_RESP" | jq -r '[.[] | select(.fileStatus == "completed" or .fileStatus == "COMPLETED")] | length')
    ERROR_COUNT=$(echo "$STATUS_RESP" | jq -r '[.[] | select(.fileStatus == "error" or .fileStatus == "disabled")] | length')
    
    if [ "$ERROR_COUNT" -gt 0 ]; then
        echo "❌ Ingestion pipelines stopped: One or more workers threw system failure codes."
        exit 1
    fi
    
    echo "System Status Check (Attempt $ATTEMPT/$MAX_ATTEMPTS): $COMPLETED_COUNT / 3 files verified as completed."
    
    if [ "$COMPLETED_COUNT" -eq 3 ]; then
        break
    fi
    
    sleep 3
    ATTEMPT=$((ATTEMPT+1))
done

if [ "$COMPLETED_COUNT" -ne 3 ]; then
    echo "❌ Timeout waiting for pipeline background workers to finalize all 3 file segments."
    exit 1
fi

# 8. Create the Unified Target Dataset Group
echo "Bundling generated file list matrix into Dataset collection [$DATASET_ID]..."
JSON_PAYLOAD="{\"accession_ids\": [$ACCESSION_IDS], \"dataset_id\": \"$DATASET_ID\", \"user\": \"$USER_ID\"}"

curl -fsS -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -X POST \
     -d "$JSON_PAYLOAD" \
     "$API_URL/dataset/create"

# 9. Release the collection to the active access pool
echo "Releasing tracking collection dataset state for validation..."
curl -fsS -H "Authorization: Bearer $TOKEN" -X POST "$API_URL/dataset/release/$DATASET_ID"

# debug 
echo "=== Debug: Listing contents of S3 bucket after creation ==="
s3cmd -c /shared/s3cfg ls s3://

echo "=== 🎉 Success! Dataset '$DATASET_ID' is initialized with 3 clean targets, structured correctly, and ready for rotation! ==="