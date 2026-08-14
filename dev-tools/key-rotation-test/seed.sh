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
apt-get -o DPkg::Lock::Timeout=60 install -y s3cmd curl jq postgresql-client util-linux bsdmainutils > /dev/null

# Clean and prepare a temporary storage playground
TMP_DATA_DIR=/tmp/test-data
mkdir -p "$TMP_DATA_DIR"
rm -f "$TMP_DATA_DIR"/*


# Process, encrypt, and upload each file individually
COUNTER=101
for filename in $FILES; do
    echo "--------------------------------------------------"
    echo "Processing local file: $filename"

    # Write unique dummy content inside each file
    echo "Confidential genomic sequencing data block for $filename" > "$TMP_DATA_DIR/$filename"

    yes | /shared/crypt4gh encrypt \
      -p /shared/c4gh.pub.pem \
      -f "$TMP_DATA_DIR/$filename"

    # Construct exact target bucket structural destination path
    S3_TARGET_URI="s3://$BUCKET_TARGET/$DATASET_FOLDER/$filename.c4gh"
    API_INBOX_PATH="$DATASET_FOLDER/$filename.c4gh"

    echo "Uploading via s3cmd to: $S3_TARGET_URI"
    s3cmd -c /shared/s3cfg put "$TMP_DATA_DIR/$filename.c4gh" "$S3_TARGET_URI"

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

echo "=== 🎉 Success! Dataset '$DATASET_ID' is initialized with 4 clean targets, structured correctly, and ready for rotation! ==="

# ==============================================================================
# Seed 5,000 Valid Crypt4GH Headers for long background key rotation test
# ==============================================================================
BG_DATASET_ID="EGAD00000000099"
echo "Generating 5,000 valid Crypt4GH headers..."
echo "A" > "$TMP_DATA_DIR/dummy.txt"

yes | /shared/crypt4gh encrypt \
    -p /shared/c4gh.pub.pem \
    -f "$TMP_DATA_DIR/dummy.txt"

if [ ! -f "/shared/c4gh.pub.pem" ]; then
    echo "❌ Error: Public key /shared/c4gh.pub.pem not found!"
    exit 1
fi

# Extract key_hash
KEY_HASH=$(cat /shared/c4gh.pub.pem | awk 'NR==2' | base64 -d | xxd -p -c256 | tr -d '\r\n[:space:]')

# Extract header hex bytes without xxd (using hexdump or od as fallback)
if command -v hexdump >/dev/null 2>&1; then
    HEADER_HEX=$(head -c 124 "$TMP_DATA_DIR/dummy.txt.c4gh" | hexdump -v -e '/1 "%02x"')
elif command -v od >/dev/null 2>&1; then
    HEADER_HEX=$(head -c 124 "$TMP_DATA_DIR/dummy.txt.c4gh" | od -An -tx1 | tr -d ' \n')
else
    echo "❌ Error: Neither hexdump nor od is available for hex conversion."
    exit 1
fi

echo "Inserting 5,000 file records into Postgres matching schema main.sql..."

PGPASSWORD=rootpasswd psql -U postgres -h postgres -d sda <<EOF
BEGIN;

-- 1. Ensure the key_hash from c4gh.pub.pem exists in sda.encryption_keys
INSERT INTO sda.encryption_keys (key_hash, description)
VALUES ('$KEY_HASH', 'this is the c4gh key')
ON CONFLICT (key_hash) DO UPDATE SET description = EXCLUDED.description;

-- 2. Create dataset record if not existing
INSERT INTO sda.datasets (stable_id, title, description)
VALUES ('$BG_DATASET_ID', 'Benchmark Dataset 5K', 'Key rotation load test dataset')
ON CONFLICT (stable_id) DO NOTHING;

-- 3. Create temp table starting at index 20001 to avoid EGAF collisions
CREATE TEMP TABLE seed_files AS
SELECT
    gen_random_uuid() AS id,
    'EGAF' || LPAD(gs::text, 11, '0') AS accession_id,
    'dataset_folder/bg_file_' || gs || '.txt.c4gh' AS submission_file_path,
    '$USER_ID' AS submission_user,
    'ready' AS last_event,
    '/archive/bg_file_' || gs || '.txt.c4gh' AS archive_file_path,
    1024 AS archive_file_size,
    '$KEY_HASH' AS key_hash
FROM generate_series(20001, 25000) gs; -- 5,000 files

-- 4. Bulk Insert into sda.files with real key_hash
INSERT INTO sda.files (
    id,
    stable_id,
    submission_file_path,
    submission_user,
    last_event,
    archive_file_path,
    archive_file_size,
    header,
    key_hash
)
SELECT
    id,
    accession_id,
    submission_file_path,
    submission_user,
    last_event,
    archive_file_path,
    archive_file_size,
    '$HEADER_HEX',
    key_hash
FROM seed_files;

-- 5. Bulk Insert File Accessions/References
INSERT INTO sda.file_references (file_id, reference_id, reference_scheme)
SELECT id, accession_id, 'EGA'
FROM seed_files;

-- 6. Link Files to Dataset
INSERT INTO sda.file_dataset (file_id, dataset_id)
SELECT sf.id, d.id
FROM seed_files sf
CROSS JOIN sda.datasets d
WHERE d.stable_id = '$BG_DATASET_ID';

-- 7. Add Checksums Required by GetReVerificationData
-- Inserts dummy SHA256 checksums for UNENCRYPTED and ARCHIVED sources
INSERT INTO sda.checksums (file_id, checksum, type, source)
SELECT id, 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'SHA256', 'UNENCRYPTED'
FROM seed_files;

INSERT INTO sda.checksums (file_id, checksum, type, source)
SELECT id, 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'SHA256', 'ARCHIVED'
FROM seed_files;

COMMIT;
EOF

echo "=== 🎉 Success! Dataset '$BG_DATASET_ID' initialized with 5,000 valid Crypt4GH headers matching sda schema! ==="
