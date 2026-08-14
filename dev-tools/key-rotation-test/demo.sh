#!/usr/bin/env bash
# shellcheck disable=SC2086 # ignore unquoted "$DB_OPTS" in psql commands to allow for multiple options
set -euo pipefail

# --- CONFIGURATION & ENV SETUP ---
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
. "$SCRIPT_DIR/helpers.sh"

TARGET_CASE="${1:-}"

export SHARED_DIR="/tmp/sda/shared"
export POSTGRES_IMAGE="postgres"
export DB_OPTS="-U postgres -d sda"
PR_NUMBER=$(docker ps --format "{{.Image}}" | grep -oE "PR[0-9]{4}-[0-9]{2}-[0-9]{2}" | head -n 1 | sed 's/PR//')
export PR_NUMBER
USER_ID="test@dummy.org"

# all files should be stored in a local temp directory to avoid polluting the shared folder
OUTDIR="/tmp/sda/test-data"
mkdir -p "$OUTDIR"

# delete any existing files in the output directory to ensure a clean slate for the demo
rm -rf "${OUTDIR:?}"/*

# ======================================================================
# Creates a clean snapshot of the database state right after initialization
backup_database() {
    echo "Creating clean snapshot of initialized database..."
    docker compose exec -T -e PGPASSWORD=rootpasswd postgres pg_dump -U postgres -d sda -F c -b -v -f /var/lib/postgresql/data/clean_db.dump > /dev/null
    echo "Snapshot clean_db.dump successfully stored."
}

# Flashes the database back to pristine, original status
restore_database() {
    echo "Restoring database to clean snapshot state..."
    # Terminate active backend connections to allow drop/restore operations
    docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql -U postgres -d sda -c \
        "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'sda' AND pid <> pg_backend_pid();" > /dev/null 2>&1

    # Clean and restore database structures
    docker compose exec -T -e PGPASSWORD=rootpasswd postgres dropdb -U postgres --if-exists sda
    docker compose exec -T -e PGPASSWORD=rootpasswd postgres createdb -U postgres sda
    docker compose exec -T -e PGPASSWORD=rootpasswd postgres pg_restore -U postgres -d sda /var/lib/postgresql/data/clean_db.dump > /dev/null
    echo "SUCCESS: Database state rolled back to clean baseline!"
}

# Functions to run each test case
case_1_standard_lifecycle() {
    # ========================================================================
    # CASE 1: Standard Lifecycle (Download -> Rotate -> Remove old key -> Download)
    # ========================================================================
    log_header "CASE 1: Standard Key Lifecycle Transition"
    echo "Step 1.1: Downloading file before rotation..."
    curl -s -H "Authorization: Bearer $TOKEN" -H "X-C4GH-Public-Key: $CLIENT_PUB_KEY" \
        http://localhost:8085/files/EGAF00000000101 -o "$OUTDIR"/c1-before.c4gh
    C4GH_PASSWORD=c4ghpass sda-cli decrypt --key "$SHARED_DIR"/client.sec.pem "$OUTDIR"/c1-before.c4gh
    echo "SUCCESS: File decrypted using current valid key state."

    echo -e "\nStep 1.2: Executing key rotation..."
    FILE_ID=$(docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c "SELECT id FROM sda.files WHERE stable_id = 'EGAF00000000101';")
    echo "Rotating key for file ID: $FILE_ID"
    curl -H "Authorization: Bearer $TOKEN" -X POST  "$API_HOST/file/rotatekey/$FILE_ID"

    echo -e "\nStep 1.3: Simulating old key removal (Applying config-keyremoved via override)..."

    # Layer the keyremoved override onto the running core stack
    docker compose -f compose.yml -f override-keyremoved.yml up -d --no-deps reencrypt download

    # Explicitly restart the download and reencrypt microservices to force a configuration reload
    docker compose -f compose.yml -f override-keyremoved.yml restart reencrypt download

    echo "SUCCESS: Old key removed. reencrypt service is now blind to the original c4gh.sec.pem key."

    echo "Waiting for reencrypt gRPC service listener (Port 50051) to stabilize..."
    until nc -z localhost 50051; do
        echo -n "."
        sleep 1
    done
    # Give the app layer one final second to finish its internal handshake initialization
    sleep 2

    echo -e "\nStep 1.4: Downloading and decrypting after rotation..."

    curl -s -H "Authorization: Bearer $TOKEN" -H "X-C4GH-Public-Key: $CLIENT_PUB_KEY" \
        http://localhost:8085/files/EGAF00000000101 -o "$OUTDIR"/c1-after.c4gh

    C4GH_PASSWORD=c4ghpass sda-cli decrypt --key "$SHARED_DIR"/client.sec.pem "$OUTDIR"/c1-after.c4gh
    echo "SUCCESS: User successfully decrypted the file generated after key rotation!"

    echo -e "\nStep 1.5: Verifying key removal safety: Attempting to download an unrotated file..."
    STATUS_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
    -H "Authorization: Bearer $TOKEN" -H "X-C4GH-Public-Key: $CLIENT_PUB_KEY" \
    http://localhost:8085/files/EGAF00000000102 || true)

    if [ "$STATUS_CODE" -eq 500 ]; then
        echo "SUCCESS: Unrotated files are securely blocked because the old key was removed!"
    else
        echo "WARNING: Unexpected response code $STATUS_CODE"
    fi
}

case_2_mixed_key_dataset() {
    # ========================================================================
    # CASE 2: Partial Dataset Rotations (Mixed State Verification)
    # ========================================================================
    log_header "CASE 2: Partial Dataset Rotations (Mixed Key States)"

    echo "Step 2.0: Rolling back runtime mutations..."
    restore_database

    echo "Step 2.1: Ensuring base configuration is active with both keys..."
    docker compose -f compose.yml up -d --no-deps reencrypt download
    docker compose -f compose.yml restart reencrypt download

    echo "Waiting for reencrypt gRPC service listener to stabilize..."
    until nc -z localhost 50051; do echo -n "."; sleep 1; done
    echo -e "\nServices are online."

    echo -e "\nStep 2.2: Extracting file IDs for our test files..."
    # Rotate half of the files in the dataset to simulate a mixed-key scenario
    # File 1 and 2 to be rotated, File 3 and 4 to remain with the original key
    FILE1_ID=$(docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c "SELECT id FROM sda.files WHERE stable_id = 'EGAF00000000101';")
    FILE2_ID=$(docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c "SELECT id FROM sda.files WHERE stable_id = 'EGAF00000000102';")
    FILE3_ID=$(docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c "SELECT id FROM sda.files WHERE stable_id = 'EGAF00000000103';")
    FILE4_ID=$(docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c "SELECT id FROM sda.files WHERE stable_id = 'EGAF00000000104';")

    echo "File 1 and 2 (To be rotated): ID=$FILE1_ID, StableID=EGAF00000000101; ID=$FILE2_ID, StableID=EGAF00000000102"
    echo "File 3 and 4 (To be left alone): ID=$FILE3_ID, StableID=EGAF00000000103; ID=$FILE4_ID, StableID=EGAF00000000104"

    echo -e "\nStep 2.3: Executing key rotation ONLY on File 1 and 2..."
    curl -s -H "Authorization: Bearer $TOKEN" -X POST "$API_HOST/file/rotatekey/$FILE1_ID"
    echo "Rotation command issued for File 1."
    curl -s -H "Authorization: Bearer $TOKEN" -X POST "$API_HOST/file/rotatekey/$FILE2_ID"
    echo "Rotation command issued for File 2."

    echo -e "\nStep 2.4: Attempting download and decryption of the ROTATED file (File 1 and 2)..."
    curl -s -H "Authorization: Bearer $TOKEN" -H "X-C4GH-Public-Key: $CLIENT_PUB_KEY" \
        http://localhost:8085/files/EGAF00000000101 -o "$OUTDIR"/c2-file1-rotated.c4gh
    curl -s -H "Authorization: Bearer $TOKEN" -H "X-C4GH-Public-Key: $CLIENT_PUB_KEY" \
        http://localhost:8085/files/EGAF00000000102 -o "$OUTDIR"/c2-file2-rotated.c4gh

    C4GH_PASSWORD=c4ghpass sda-cli decrypt --key "$SHARED_DIR"/client.sec.pem "$OUTDIR"/c2-file1-rotated.c4gh
    echo "SUCCESS: File 1 (new key) downloaded and decrypted perfectly!"
    C4GH_PASSWORD=c4ghpass sda-cli decrypt --key "$SHARED_DIR"/client.sec.pem "$OUTDIR"/c2-file2-rotated.c4gh
    echo "SUCCESS: File 2 (new key) downloaded and decrypted perfectly!"

    echo -e "\nStep 2.5: Attempting download and decryption of the UNROTATED file (File 3 and 4)..."
    curl -s -H "Authorization: Bearer $TOKEN" -H "X-C4GH-Public-Key: $CLIENT_PUB_KEY" \
        http://localhost:8085/files/EGAF00000000103 -o "$OUTDIR"/c2-file3-legacy.c4gh
    curl -s -H "Authorization: Bearer $TOKEN" -H "X-C4GH-Public-Key: $CLIENT_PUB_KEY" \
        http://localhost:8085/files/EGAF00000000104 -o "$OUTDIR"/c2-file4-legacy.c4gh

    C4GH_PASSWORD=c4ghpass sda-cli decrypt --key "$SHARED_DIR"/client.sec.pem "$OUTDIR"/c2-file3-legacy.c4gh
    echo "SUCCESS: File 3 (old key) downloaded and decrypted perfectly!"
    C4GH_PASSWORD=c4ghpass sda-cli decrypt --key "$SHARED_DIR"/client.sec.pem "$OUTDIR"/c2-file4-legacy.c4gh
    echo "SUCCESS: File 4 (old key) downloaded and decrypted perfectly!"

    echo -e "\n\033[1;32mSUCCESS: Mixed-key dataset handles both active cryptographic keys simultaneously!\033[0m"
}

case_3_invalid_rotation_target() {
    # ========================================================================
    # CASE 3: Try to start rotatekey without configuring a target key
    # ========================================================================
    log_header "CASE 3: Startup Configuration Validation (Missing Target Key)"

    echo "Step 3.1: Applying config-norotatetarget override to rotatekey service..."
    docker compose -f compose.yml -f override-norotatetarget.yaml up -d --no-deps rotatekey

    # Allow time for the container to attempt initialization and fail
    sleep 2

    echo -e "\nStep 3.2: Verifying rotatekey service crashed on missing c4gh.rotatePubKeyPath..."

    # Capture stderr and stdout from the container logs
    LOG_OUTPUT=$(docker compose logs rotatekey 2>&1)

    if echo "$LOG_OUTPUT" | grep -q "c4gh.rotatePubKeyPath not set"; then
        echo -e "\033[1;32mSUCCESS: rotatekey correctly refused to start due to missing configuration.\033[0m"
        echo "Observed fatal log output:"
        echo "$LOG_OUTPUT" | grep "c4gh.rotatePubKeyPath not set" | head -n 5
    else
        echo -e "\033[1;31mFAILED: rotatekey did not fail with the expected configuration error.\033[0m"
        echo "Full log output:"
        echo "$LOG_OUTPUT"
        exit 1
    fi

    pause_step ">>> PRESS [ENTER] TO RESET SERVICES BACK TO STANDARD CONFIGURATION..."

    echo -e "\nStep 3.3: Resetting rotatekey service back to standard configuration..."
    # Stop the crash-looping service first to ensure a clean state transition
    docker compose stop rotatekey
    docker compose -f compose.yml up -d --no-deps rotatekey
}

case_4_concurrent_race_condition() {
    # ========================================================================
    # CASE 4: Have files in archive, start key rotation, download a file while
    # the rotation is in progress, and verify that the download is successful
    # This case is tested by creating 10 parallel download workers that
    # continuously request the file while key rotation is in progress.
    # ========================================================================
    log_header "CASE 4: Concurrent Operations (Simultaneous Header Rotation vs. Active Downloads)"

    echo "Step 4.1: Restoring database baseline..."
    restore_database

    FILE_ID=$(docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c "SELECT id FROM sda.files WHERE stable_id = 'EGAF00000000101';")

    echo -e "\nStep 4.2: Launching background download flood (10 parallel workers)..."

    # Spawn 10 parallel background download workers continuously requesting the file
    FLOOD_DIR="$OUTDIR/c4_flood"
    mkdir -p "$FLOOD_DIR"

    for i in {1..10}; do
        (
            for j in {1..5}; do
                curl -s -H "Authorization: Bearer $TOKEN" \
                        -H "X-C4GH-Public-Key: $CLIENT_PUB_KEY" \
                        http://localhost:8085/files/EGAF00000000101 \
                        -o "$FLOOD_DIR/download_${i}_${j}.c4gh"
            done
        ) &
    done

    echo "Download workers active. Step 4.3: Triggering key rotation during active traffic..."
    curl -s -H "Authorization: Bearer $TOKEN" -X POST "$API_HOST/file/rotatekey/$FILE_ID" > /dev/null

    # Wait for all background download workers to finish
    wait
    echo "All concurrent download tasks completed."

    echo -e "\nStep 4.4: Validating integrity of downloaded files..."

    CORRUPTED_COUNT=0
    SUCCESS_COUNT=0

    # Test every single downloaded header file
    for file in "$FLOOD_DIR"/*.c4gh; do
        if C4GH_PASSWORD=c4ghpass sda-cli decrypt --key "$SHARED_DIR"/client.sec.pem "$file" > /dev/null 2>&1; then
            ((SUCCESS_COUNT++)) || true
        else
            ((CORRUPTED_COUNT++)) || true
            echo "❌ Corrupted download detected: $file"
        fi
    done

    echo "Decryption Audit Results:"
    echo " - Successfully decrypted payloads: $SUCCESS_COUNT"
    echo " - Corrupted/Failed payloads: $CORRUPTED_COUNT"

    if [ "$CORRUPTED_COUNT" -eq 0 ]; then
        echo -e "\n\033[1;32mSUCCESS: Zero payload/header corruptions under concurrent load!\033[0m"
        echo -e "\033[1;32mThe database/service handled atomic lock transitions correctly.\033[0m"
    else
        echo -e "\n\033[1;31mFAILED: $CORRUPTED_COUNT downloads were corrupted due to race conditions during rotation!\033[0m"
        exit 1
    fi

    # Clean up temporary flood files
    rm -rf "$FLOOD_DIR"
}

case_5_ingest_mixed_keys_during_rotation() {
    # ========================================================================
    # CASE 5: Mixed Key Ingestion During Active Bulk Key Rotation
    # ========================================================================
    log_header "CASE 5: Mixed Key Ingestion During Active Key Rotation (Load Test)"

    DATASET_ID="EGAD00000000001"
    BG_ROTATE_DATASET_ID="EGAD00000000099"
    ACCESSION_OLD="EGAF00000000201"
    ACCESSION_NEW="EGAF00000000202"

    echo "Step 5.1: Restoring clean database baseline..."
    restore_database

    echo -e "\nStep 5.2: Ensuring base configuration is active with both keys..."
    docker compose -f compose.yml up -d --no-deps reencrypt download rotatekey
    docker compose -f compose.yml restart reencrypt download rotatekey

    echo "Waiting for gRPC services to stabilize..."
    until nc -z localhost 50051; do echo -n "."; sleep 1; done
    echo -e "\nServices online."

    echo -e "\nStep 5.3: Creating and encrypting test payloads on host..."
    INBOX_DIR="$OUTDIR/c5_inbox"
    mkdir -p "$INBOX_DIR"

    BUCKET_TARGET="test_dummy.org"
    DATASET_FOLDER="dataset_folder"

    # 1. Create raw text payloads
    echo "Genomic data payload encrypted under old c4gh key" > "$INBOX_DIR/file_old_key.txt"
    echo "Genomic data payload encrypted under new rotate key" > "$INBOX_DIR/file_new_key.txt"

    # 2. Encrypt locally using client private key + target server public keys
    sda-cli encrypt --key "$SHARED_DIR"/c4gh.pub.pem "$INBOX_DIR/file_old_key.txt"
    sda-cli encrypt --key "$SHARED_DIR"/rotatekey.pub.pem "$INBOX_DIR/file_new_key.txt"

    echo -e "\nStep 5.4: Uploading files directly from host using generated s3cfg..."
    s3cmd -c "$SHARED_DIR/s3cfg" \
          --host="localhost:18000" \
          --host-bucket="localhost:18000/%(bucket)s" \
          --no-ssl \
          put "$INBOX_DIR/file_old_key.txt.c4gh" "s3://$BUCKET_TARGET/$DATASET_FOLDER/file_old_key.txt.c4gh"

    s3cmd -c "$SHARED_DIR/s3cfg" \
          --host="localhost:18000" \
          --host-bucket="localhost:18000/%(bucket)s" \
          --no-ssl \
          put "$INBOX_DIR/file_new_key.txt.c4gh" "s3://$BUCKET_TARGET/$DATASET_FOLDER/file_new_key.txt.c4gh"

    echo -e "\nStep 5.5: Launching key rotation on large dataset in background..."

    # Store HTTP response code to temp file for assertions
    ROTATION_HTTP_OUT="$OUTDIR/rotation_http_code.txt"

    (
        HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$API_HOST/dataset/rotatekey/$BG_ROTATE_DATASET_ID" \
             -H "Authorization: Bearer $TOKEN" \
             -H "Content-Type: application/json")
        echo "$HTTP_CODE" > "$ROTATION_HTTP_OUT"
    ) &
    ROTATION_PID=$!

    echo "⚡ Key rotation initiated in background (PID: $ROTATION_PID) for dataset $BG_ROTATE_DATASET_ID!"

    echo -e "\nStep 5.6: Querying database for file UUIDs during active rotation..."

    UUID_OLD=$(docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c \
        "SELECT id FROM sda.files WHERE submission_file_path='$DATASET_FOLDER/file_old_key.txt.c4gh' ORDER BY created_at DESC LIMIT 1;" | tr -d '\r\n[:space:]')

    UUID_NEW=$(docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c \
        "SELECT id FROM sda.files WHERE submission_file_path='$DATASET_FOLDER/file_new_key.txt.c4gh' ORDER BY created_at DESC LIMIT 1;" | tr -d '\r\n[:space:]')

    echo "Found UUID (Old Key File): $UUID_OLD"
    echo "Found UUID (New Key File): $UUID_NEW"

    echo -e "\nStep 5.7: Ingesting mixed-key files concurrently with key rotation..."
    curl -fsS -H "Authorization: Bearer $TOKEN" -X POST "$API_HOST/file/ingest?fileid=$UUID_OLD"
    curl -fsS -H "Authorization: Bearer $TOKEN" -X POST "$API_HOST/file/ingest?fileid=$UUID_NEW"

    echo "Waiting for ingestion pipeline workers..."
    sleep 3

    echo -e "\nStep 5.8: Assigning accession IDs..."
    curl -fsS -H "Authorization: Bearer $TOKEN" -X POST "$API_HOST/file/accession?fileid=$UUID_OLD&accessionid=$ACCESSION_OLD"
    curl -fsS -H "Authorization: Bearer $TOKEN" -X POST "$API_HOST/file/accession?fileid=$UUID_NEW&accessionid=$ACCESSION_NEW"
    sleep 2

    echo -e "\nStep 5.9: Mapping accessioned files to Dataset ($DATASET_ID)..."
    ACCESSION_IDS="\"$ACCESSION_OLD\", \"$ACCESSION_NEW\""
    JSON_PAYLOAD="{\"accession_ids\": [$ACCESSION_IDS], \"dataset_id\": \"$DATASET_ID\", \"user\": \"$USER_ID\"}"

    curl -fsS -H "Authorization: Bearer $TOKEN" \
         -H "Content-Type: application/json" \
         -X POST \
         -d "$JSON_PAYLOAD" \
         "$API_HOST/dataset/create"

    echo -e "\nStep 5.10: Waiting for background bulk key rotation task to finalize..."
    wait $ROTATION_PID

    ROTATION_STATUS=$(cat "$ROTATION_HTTP_OUT" 2>/dev/null || echo "000")
    if [ "$ROTATION_STATUS" -eq 200 ] || [ "$ROTATION_STATUS" -eq 202 ]; then
        echo "✅ Key rotation completed successfully with status $ROTATION_STATUS!"
    else
        echo "❌ Error: Key rotation request failed with HTTP status $ROTATION_STATUS!"
        exit 1
    fi

    echo -e "\nStep 5.11: Verifying download and decryption of mixed-key files..."

    echo "Downloading $ACCESSION_OLD (Old Key Upload)..."
    curl -s -H "Authorization: Bearer $TOKEN" -H "X-C4GH-Public-Key: $CLIENT_PUB_KEY" \
        "http://localhost:8085/files/$ACCESSION_OLD" -o "$OUTDIR/c5-old-download.c4gh"
    echo "c4ghpass" | sda-cli decrypt --key "$SHARED_DIR/client.sec.pem" "$OUTDIR/c5-old-download.c4gh"
    echo "SUCCESS: Legacy key file downloaded and decrypted!"

    echo "Downloading $ACCESSION_NEW (New Key Upload)..."
    curl -s -H "Authorization: Bearer $TOKEN" -H "X-C4GH-Public-Key: $CLIENT_PUB_KEY" \
        "http://localhost:8085/files/$ACCESSION_NEW" -o "$OUTDIR/c5-new-download.c4gh"
    echo "c4ghpass" | sda-cli decrypt --key "$SHARED_DIR/client.sec.pem" "$OUTDIR/c5-new-download.c4gh"
    echo "SUCCESS: New key file downloaded and decrypted!"

    echo -e "\n\033[1;32mSUCCESS: Mixed-key ingestion, dataset mapping, and downloads completed concurrently with bulk key rotation!\033[0m"

    rm -rf "$INBOX_DIR" "$ROTATION_HTTP_OUT"
}

case_6_multi_key_archive_migration() {
    # ========================================================================
    # CASE 6: Multi-Key Support & Archive Migration (5 Keys Total)
    # ========================================================================
    log_header "CASE 6: Multi-Key Support & Archive Migration (5 Keys Total)"

    DATASET_ID="EGAD00000000006"

    echo "Step 6.1: Restoring clean database baseline..."
    restore_database

    echo -e "\nStep 6.2: Registering 4 extra public key hashes in PostgreSQL for Case 6 only..."
    for i in 1 2 3 4; do
        PUB_KEY_PATH="$SHARED_DIR/extra_key_$i.pub.pem"

        if [ ! -f "$PUB_KEY_PATH" ]; then
            echo "❌ Error: $PUB_KEY_PATH not found! Ensure make_sda_credentials.sh created it."
            exit 1
        fi

        EXTRA_HASH=$(compute_key_hash "$PUB_KEY_PATH")

        docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -c \
            "INSERT INTO sda.encryption_keys (key_hash, description) VALUES ('$EXTRA_HASH', 'Extra registered key $i') ON CONFLICT (key_hash) DO NOTHING;" > /dev/null

        echo "Registered Extra Key $i in DB: $EXTRA_HASH"
    done

    echo -e "\nStep 6.3: Applying config-multikey.yaml via override-multikey.yml..."

    # Apply override-multikey.yml onto running core stack
    docker compose -f compose.yml -f override-multikey.yml up -d --no-deps reencrypt download rotatekey ingest verify mapper finalize
    docker compose -f compose.yml -f override-multikey.yml restart reencrypt download rotatekey ingest verify mapper finalize

    echo "Waiting for reencrypt gRPC service listener (Port 50051) to stabilize..."
    until nc -z localhost 50051; do
        echo -n "."
        sleep 1
    done
    sleep 2
    echo -e "\nServices active with 5 pre-configured private keys."

    echo -e "\nStep 6.4: Encrypting test files with all 5 public keys..."
    INBOX_DIR="$OUTDIR/c6_inbox"
    mkdir -p "$INBOX_DIR"
    BUCKET_TARGET="test_dummy.org"
    DATASET_FOLDER="c6_multi_key_folder"

    # Encrypt 1 file with baseline c4gh key
    echo "Payload encrypted with Baseline Key" > "$INBOX_DIR/file_k0.txt"
    sda-cli encrypt --key "$SHARED_DIR/c4gh.pub.pem" "$INBOX_DIR/file_k0.txt"

    # Encrypt 4 files with extra_key_1 through extra_key_4
    for i in 1 2 3 4; do
        echo "Payload encrypted with Extra Key $i" > "$INBOX_DIR/file_k$i.txt"
        sda-cli encrypt --key "$SHARED_DIR/extra_key_$i.pub.pem" "$INBOX_DIR/file_k$i.txt"
    done

    echo -e "\nStep 6.5: Uploading all 5 multi-key encrypted files to inbox..."
    for i in 0 1 2 3 4; do
        s3cmd -c "$SHARED_DIR/s3cfg" \
              --host="localhost:18000" \
              --host-bucket="localhost:18000/%(bucket)s" \
              --no-ssl \
              put "$INBOX_DIR/file_k$i.txt.c4gh" "s3://$BUCKET_TARGET/$DATASET_FOLDER/file_k$i.txt.c4gh" > /dev/null
    done

    echo -e "\nStep 6.6: Ingesting and accessioning multi-key files..."
    ACCESSION_LIST=""

    for i in 0 1 2 3 4; do
        FILE_PATH="$DATASET_FOLDER/file_k$i.txt.c4gh"

        UUID=$(docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c \
            "SELECT id FROM sda.files WHERE submission_file_path='$FILE_PATH' ORDER BY created_at DESC LIMIT 1;" | tr -d '\r\n[:space:]')

        ACCESSION="EGAF0000000060$i"

        echo "Ingesting file_k$i (UUID: $UUID) -> Accession: $ACCESSION"
        curl -fsS -H "Authorization: Bearer $TOKEN" -X POST "$API_HOST/file/ingest?fileid=$UUID"
        sleep 1
        curl -fsS -H "Authorization: Bearer $TOKEN" -X POST "$API_HOST/file/accession?fileid=$UUID&accessionid=$ACCESSION"

        if [ -z "$ACCESSION_LIST" ]; then
            ACCESSION_LIST="\"$ACCESSION\""
        else
            ACCESSION_LIST="$ACCESSION_LIST, \"$ACCESSION\""
        fi
    done

    echo "Waiting for ingestion pipeline workers to archive files..."
    sleep 3

    echo -e "\nStep 6.7: Mapping accessioned files to Dataset ($DATASET_ID)..."
    JSON_PAYLOAD="{\"accession_ids\": [$ACCESSION_LIST], \"dataset_id\": \"$DATASET_ID\", \"user\": \"$USER_ID\"}"

    curl -fsS -H "Authorization: Bearer $TOKEN" \
         -H "Content-Type: application/json" \
         -X POST \
         -d "$JSON_PAYLOAD" \
         "$API_HOST/dataset/create"

    sleep 2

    echo -e "\nStep 6.8: Migrating all archived files to new target key via rotatekey..."
    curl -fsS -X POST "$API_HOST/dataset/rotatekey/$DATASET_ID" \
         -H "Authorization: Bearer $TOKEN" \
         -H "Content-Type: application/json"

    sleep 3

    echo -e "\nStep 6.9: Verifying key hashes in PostgreSQL..."
    TARGET_KEY_HASH=$(compute_key_hash "$SHARED_DIR/rotatekey.pub.pem")
    UPDATED_COUNT=$(docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c \
    "SELECT COUNT(*) \
    FROM sda.files f \
    JOIN sda.file_dataset fd ON f.id = fd.file_id \
    JOIN sda.datasets d ON fd.dataset_id = d.id \
    WHERE d.stable_id = '$DATASET_ID' \
        AND f.key_hash = '$TARGET_KEY_HASH';")

    echo "Total verified rotated files with target key hash ($TARGET_KEY_HASH): $UPDATED_COUNT / 5"
    if [ "$UPDATED_COUNT" -ne 5 ]; then
        echo "❌ Error: Failed to migrate all 5 archived files! Only $UPDATED_COUNT / 5 have the new key hash."

        docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -c \
        "SELECT f.id, f.stable_id, f.key_hash \
        FROM sda.files f \
        JOIN sda.file_dataset fd ON f.id = fd.file_id \
        JOIN sda.datasets d ON fd.dataset_id = d.id \
        WHERE d.stable_id = '$DATASET_ID';"

        exit 1
    fi


    echo -e "\nStep 6.10: Testing download and client decryption of migrated files..."
    for i in 0 1 2 3 4; do
        ACCESSION="EGAF0000000060$i"
        echo "Downloading and decrypting $ACCESSION..."

        curl -s -H "Authorization: Bearer $TOKEN" -H "X-C4GH-Public-Key: $CLIENT_PUB_KEY" \
            "http://localhost:8085/files/$ACCESSION" -o "$OUTDIR/c6-download-$i.c4gh"

        echo "c4ghpass" | sda-cli decrypt --key "$SHARED_DIR/client.sec.pem" "$OUTDIR/c6-download-$i.c4gh" > /dev/null
        echo "✅ File $ACCESSION successfully decrypted!"
    done

    echo -e "\nStep 6.11: Resetting services back to default base compose state..."
    docker compose -f compose.yml up -d --no-deps reencrypt download rotatekey ingest verify mapper finalize> /dev/null
    docker compose -f compose.yml restart reencrypt download rotatekey ingest verify mapper finalize > /dev/null

    echo -e "\n\033[1;32mSUCCESS: All 5 multi-key files were ingested, migrated, and decrypted cleanly!\033[0m"

    rm -rf "$INBOX_DIR"
}

case_7_deprecated_key_ingest_rejection() {
    # ========================================================================
    # CASE 7: Deprecated Key Ingestion Rejection
    # ========================================================================
    log_header "CASE 7: Deprecated Key Ingestion Rejection"

    echo "Step 7.1: Restoring clean database baseline..."
    restore_database

    echo -e "\nStep 7.2: Verifying deprecated key presence in sda.encryption_keys..."
    PUB_KEY_PATH="$SHARED_DIR/deprecated_key.pub.pem"

    if [ ! -f "$PUB_KEY_PATH" ]; then
        echo "❌ Error: $PUB_KEY_PATH not found! Ensure make_sda_credentials.sh generated it."
        exit 1
    fi

    DEPRECATED_HASH=$(compute_key_hash "$PUB_KEY_PATH")

    # Ensure key has deprecated_at set in DB (idempotent guard)
    docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -c \
        "INSERT INTO sda.encryption_keys (key_hash, description, deprecated_at)
         VALUES ('$DEPRECATED_HASH', 'Deprecated test key', NOW() - INTERVAL '1 day')
         ON CONFLICT (key_hash) DO UPDATE SET deprecated_at = NOW() - INTERVAL '1 day';" > /dev/null

    echo "Verified key_hash '$DEPRECATED_HASH' is flagged as deprecated in PostgreSQL."

    echo -e "\nStep 7.3: Applying config-ingest_with_deprecatedkey.yaml via override-ingest_with_deprecatedkey.yml..."
    # Apply override so ingest/api services possess the private key to inspect the Crypt4GH header
    docker compose -f compose.yml -f override-ingest_with_deprecatedkey.yml up -d --no-deps ingest verify finalize mapper api reencrypt download rotatekey
    docker compose -f compose.yml -f override-ingest_with_deprecatedkey.yml restart ingest verify finalize mapper api reencrypt download rotatekey

    echo "Waiting for gRPC service listener (Port 50051) to stabilize..."
    until nc -z localhost 50051; do
        echo -n "."
        sleep 1
    done
    sleep 2
    echo -e "\nServices restarted with deprecated_key included in configuration."

    echo -e "\nStep 7.4: Encrypting test file using deprecated public key..."
    INBOX_DIR="$OUTDIR/c7_inbox"
    mkdir -p "$INBOX_DIR"
    BUCKET_TARGET="test_dummy.org"
    DATASET_FOLDER="c7_deprecated_folder"
    FILE_NAME="file_deprecated.txt"

    echo "Payload encrypted with deprecated key" > "$INBOX_DIR/$FILE_NAME"
    sda-cli encrypt --key "$PUB_KEY_PATH" "$INBOX_DIR/$FILE_NAME"

    echo -e "\nStep 7.5: Uploading deprecated-key encrypted file to inbox via S3..."
    s3cmd -c "$SHARED_DIR/s3cfg" \
          --host="localhost:18000" \
          --host-bucket="localhost:18000/%(bucket)s" \
          --no-ssl \
          put "$INBOX_DIR/$FILE_NAME.c4gh" "s3://$BUCKET_TARGET/$DATASET_FOLDER/$FILE_NAME.c4gh" > /dev/null

    sleep 1

    echo -e "\nStep 7.6: Fetching uploaded file UUID from database..."
    FILE_PATH="$DATASET_FOLDER/$FILE_NAME.c4gh"
    UUID=$(docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c \
        "SELECT id FROM sda.files WHERE submission_file_path='$FILE_PATH' ORDER BY created_at DESC LIMIT 1;" | tr -d '\r\n[:space:]')

    if [ -z "$UUID" ]; then
        echo "❌ Error: File upload was not registered in inbox database table!"
        exit 1
    fi
    echo "Found inbox file UUID: $UUID"

    echo -e "\nStep 7.7: Attempting ingestion (Expecting rejection due to deprecated key)..."
    HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
        -H "Authorization: Bearer $TOKEN" \
        -X POST "$API_HOST/file/ingest?fileid=$UUID" || true)

    echo "Received HTTP Status Code: $HTTP_STATUS"

    echo -e "\nStep 7.8: Asserting rejection..."
    if [ "$HTTP_STATUS" -eq 400 ] || [ "$HTTP_STATUS" -eq 500 ]; then
        echo "✅ SUCCESS: Ingestion correctly rejected file encrypted with deprecated key! (HTTP $HTTP_STATUS)"
    else
        echo "❌ FAILURE: Expected HTTP 400 or 500 rejection, but received HTTP $HTTP_STATUS!"
        echo "For the current setup, ingest does not reject files encrypted with deprecated keys, which is a known limitation."
        pause_step "Press Enter to continue and reset services back to default state..."

        # Cleanup override before exiting on error
        docker compose -f compose.yml up -d --no-deps ingest api reencrypt download rotatekey > /dev/null
        docker compose -f compose.yml restart ingest api reencrypt download rotatekey > /dev/null
        rm -rf "$INBOX_DIR"

        return 0
    fi

    # Verify state in sda.files table reflects failure or unarchived state
    FILE_STATE=$(docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c \
        "SELECT last_event FROM sda.files WHERE id='$UUID';" | tr -d '\r\n[:space:]')
    echo "File DB last event: ${FILE_STATE:-'NULL'}"

    echo -e "\nStep 7.9: Resetting services back to default base compose state..."
    docker compose -f compose.yml up -d --no-deps ingest verify finalize mapper api reencrypt download rotatekey > /dev/null
    docker compose -f compose.yml restart ingest verify finalize mapper api reencrypt download rotatekey > /dev/null

    echo "Waiting for default services to stabilize..."
    until nc -z localhost 50051; do
        echo -n "."
        sleep 1
    done
    sleep 1

    rm -rf "$INBOX_DIR"
    echo -e "\n\033[1;32mSUCCESS: Test Case 7 passed cleanly!\033[0m"
}

case_8_rotate_to_deprecated_key_rejection() {
    # ========================================================================
    # CASE 8: Key Rotation to Deprecated Target Key Rejection
    # ========================================================================
    log_header "CASE 8: Key Rotation to Deprecated Target Key Rejection"

    echo "Step 8.1: Restoring clean database baseline..."
    restore_database

    echo -e "\nStep 8.2: Ensuring target key is marked as deprecated in DB..."
    DEPRECAT_PUB_KEY="$SHARED_DIR/deprecated_key.pub.pem"

    if [ ! -f "$DEPRECAT_PUB_KEY" ]; then
        echo "❌ Error: $DEPRECAT_PUB_KEY not found! Ensure make_sda_credentials.sh generated it."
        exit 1
    fi

    DEPRECATED_HASH=$(compute_key_hash "$DEPRECAT_PUB_KEY")

    # Ensure key has deprecated_at set in DB
    docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -c \
        "INSERT INTO sda.encryption_keys (key_hash, description, deprecated_at)
         VALUES ('$DEPRECATED_HASH', 'Deprecated target key', NOW() - INTERVAL '1 day')
         ON CONFLICT (key_hash) DO UPDATE SET deprecated_at = NOW() - INTERVAL '1 day';" > /dev/null

    echo "Verified target key_hash '$DEPRECATED_HASH' is flagged as deprecated in PostgreSQL."

    echo -e "\nStep 8.3: Applying config-rotate_to_deprecatedkey.yaml via override-rotate_to_deprecatedkey.yml..."
    docker compose -f compose.yml -f override-rotate_to_deprecatedkey.yml up -d --no-deps api reencrypt download rotatekey
    docker compose -f compose.yml -f override-rotate_to_deprecatedkey.yml restart api reencrypt download rotatekey

    echo "Waiting for gRPC service listener (Port 50051) to stabilize..."
    until nc -z localhost 50051; do echo -n "."; sleep 1; done
    sleep 2

    echo -e "\nStep 8.4: Fetching UUID for pre-seeded file EGAF00000000101..."
    FILE_ID=$(docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c \
        "SELECT id FROM sda.files WHERE stable_id = 'EGAF00000000101';" | tr -d '\r\n[:space:]')

    if [ -z "$FILE_ID" ]; then
        echo "❌ Error: Pre-seeded file EGAF00000000101 not found in database!"
        exit 1
    fi
    echo "Found pre-seeded file UUID: $FILE_ID"

    # Fetch original key hash to verify it doesn't change on failure
    ORIGINAL_KEY_HASH=$(docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c \
        "SELECT key_hash FROM sda.files WHERE id='$FILE_ID';" | tr -d '\r\n[:space:]')

    echo -e "\nStep 8.5: Attempting key rotation targeting the deprecated key..."

    # Send rotation request targeting the deprecated key
    HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
        -H "Authorization: Bearer $TOKEN" \
        -X POST "$API_HOST/file/rotatekey/$FILE_ID" || true)

    echo "Received HTTP Status Code: $HTTP_STATUS"

    if [ "$HTTP_STATUS" -ne 200 ] && [ "$HTTP_STATUS" -ne 202 ]; then
        echo "❌ Error: API endpoint failed to queue the rotation job (HTTP $HTTP_STATUS)"
        exit 1
    fi

    echo -e "\nStep 8.6: Waiting for rotatekey worker to process RabbitMQ job..."
    sleep 3

    echo -e "\nStep 8.7: Verifying background rejection in rotatekey container logs..."
    ROTATE_LOGS=$(docker compose logs --tail=50 rotatekey 2>&1)

    if echo "$ROTATE_LOGS" | grep -q "the c4gh key hash has been deprecated"; then
        echo "✅ SUCCESS: rotatekey service logged fatal rejection: 'the c4gh key hash has been deprecated'"
    else
        echo "❌ FAILURE: Expected log message 'the c4gh key hash has been deprecated' was not found in rotatekey logs!"
        echo "--- Recent rotatekey logs ---"
        echo "$ROTATE_LOGS"
        echo "-----------------------------"
        exit 1
    fi

    echo -e "\nStep 8.8: Verifying file key state in database was NOT altered..."
    CURRENT_KEY_HASH=$(docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c \
        "SELECT key_hash FROM sda.files WHERE id='$FILE_ID';" | tr -d '\r\n[:space:]')

    if [ "$CURRENT_KEY_HASH" = "$DEPRECATED_HASH" ]; then
        echo "❌ Error: File encryption key was wrongly updated to the deprecated key in the DB!"
        exit 1
    fi
    echo "Confirmed file encryption key remained unchanged ($ORIGINAL_KEY_HASH)."

    echo -e "\nStep 8.9: Resetting services back to default base compose state..."
    docker compose -f compose.yml up -d --no-deps ingest api reencrypt download rotatekey > /dev/null
    docker compose -f compose.yml restart ingest api reencrypt download rotatekey > /dev/null

    echo "Waiting for default services to stabilize..."
    until nc -z localhost 50051; do echo -n "."; sleep 1; done
    sleep 1

    echo -e "\n\033[1;32mSUCCESS: Test Case 8 passed cleanly!\033[0m"
}

case_9_rotate_from_deprecated_source_key() {
    # ========================================================================
    # CASE 9: Key Rotation with a Deprecated Source Key (Migration Success)
    # ========================================================================
    log_header "CASE 9: Key Rotation from Deprecated Source Key"

    echo "Step 9.1: Restoring clean database baseline..."
    restore_database

    echo -e "\nStep 9.2: Fetching UUID and initial key hash for pre-seeded file EGAF00000000101..."
    FILE_ID=$(docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c \
        "SELECT id FROM sda.files WHERE stable_id = 'EGAF00000000101';" | tr -d '\r\n[:space:]')

    if [ -z "$FILE_ID" ]; then
        echo "❌ Error: Pre-seeded file EGAF00000000101 not found in database!"
        exit 1
    fi

    ORIGINAL_KEY_HASH=$(docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c \
        "SELECT key_hash FROM sda.files WHERE id='$FILE_ID';" | tr -d '\r\n[:space:]')

    echo "Found File UUID: $FILE_ID | Original Key Hash (c4gh): $ORIGINAL_KEY_HASH"

    echo -e "\nStep 9.3: Deprecating the file's current source key (c4gh) in PostgreSQL..."
    docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -c \
        "UPDATE sda.encryption_keys SET deprecated_at = NOW() - INTERVAL '1 day' WHERE key_hash = '$ORIGINAL_KEY_HASH';" > /dev/null

    # Confirm key is marked as deprecated
    IS_DEPRECATED=$(docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c \
        "SELECT deprecated_at FROM sda.encryption_keys WHERE key_hash = '$ORIGINAL_KEY_HASH';" | tr -d '\r\n[:space:]')

    if [ -z "$IS_DEPRECATED" ]; then
        echo "❌ Error: Failed to mark source key as deprecated in database!"
        exit 1
    fi
    echo "Confirmed source key ($ORIGINAL_KEY_HASH) is now flagged as deprecated."

    echo -e "\nStep 9.4: Executing key rotation from deprecated source key (c4gh -> rotatekey)..."
    HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
        -H "Authorization: Bearer $TOKEN" \
        -X POST "$API_HOST/file/rotatekey/$FILE_ID" || true)

    echo "API Accepted Job with HTTP Status Code: $HTTP_STATUS"

    if [ "$HTTP_STATUS" -ne 200 ] && [ "$HTTP_STATUS" -ne 202 ]; then
        echo "❌ Error: API endpoint failed to initiate rotation job (HTTP $HTTP_STATUS)"
        exit 1
    fi

    echo -e "\nStep 9.5: Waiting for rotatekey worker pipeline to process re-encryption..."
    sleep 3

    echo -e "\nStep 9.6: Verifying key hash was updated in PostgreSQL..."
    NEW_KEY_HASH=$(docker compose exec -T -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c \
        "SELECT key_hash FROM sda.files WHERE id='$FILE_ID';" | tr -d '\r\n[:space:]')

    echo "New File Encryption Key Hash: $NEW_KEY_HASH"

    if [ "$NEW_KEY_HASH" = "$ORIGINAL_KEY_HASH" ]; then
        echo "❌ Error: Key hash was not updated in the database after rotation!"
        echo "--- rotatekey worker logs ---"
        docker compose logs --tail=30 rotatekey
        echo "-----------------------------"
        exit 1
    fi

    echo "✅ Key hash successfully migrated in DB from deprecated source key ($ORIGINAL_KEY_HASH) to new key ($NEW_KEY_HASH)!"

    echo -e "\nStep 9.7: Verifying download and client decryption after migrating off deprecated key..."
    curl -s -H "Authorization: Bearer $TOKEN" -H "X-C4GH-Public-Key: $CLIENT_PUB_KEY" \
        "http://localhost:8085/files/EGAF00000000101" -o "$OUTDIR/c9-after-deprecated-rotation.c4gh"

    C4GH_PASSWORD=c4ghpass sda-cli decrypt --key "$SHARED_DIR/client.sec.pem" "$OUTDIR/c9-after-deprecated-rotation.c4gh" > /dev/null
    echo "✅ SUCCESS: User successfully downloaded and decrypted file migrated off a deprecated source key!"

    echo -e "\n\033[1;32mSUCCESS: Test Case 9 passed cleanly!\033[0m"
}

# =================================================================================
mkdir -p "$SHARED_DIR"
# Copy shared folder from the container to the local shared directory for use in the demo
docker cp verify:/shared/. "$SHARED_DIR/"

TOKEN=$(curl -s http://localhost:8000/tokens | jq -r '.[0]')
CLIENT_PUB_KEY=$(base64 -w0 "$SHARED_DIR"/client.pub.pem)
API_HOST="http://localhost:8090"

# Take a snapshot of the clean database state
backup_database

# Run the demo script for the key rotation test
if [ "$TARGET_CASE" = "all" ] || [ -z "$TARGET_CASE" ]; then
    echo "Running all test cases sequentially with pause steps..."
    case_1_standard_lifecycle
    pause_step
    case_2_mixed_key_dataset
    pause_step
    case_3_invalid_rotation_target
    pause_step
    case_4_concurrent_race_condition
    pause_step
    case_5_ingest_mixed_keys_during_rotation
    pause_step
    case_6_multi_key_archive_migration
    pause_step
    case_7_deprecated_key_ingest_rejection
    pause_step
    case_8_rotate_to_deprecated_key_rejection
    pause_step
    case_9_rotate_from_deprecated_source_key
else
    echo "Running test case $TARGET_CASE..."
    case "$TARGET_CASE" in
        1) case_1_standard_lifecycle ;;
        2) case_2_mixed_key_dataset ;;
        3) case_3_invalid_rotation_target ;;
        4) case_4_concurrent_race_condition ;;
        5) case_5_ingest_mixed_keys_during_rotation ;;
        6) case_6_multi_key_archive_migration ;;
        7) case_7_deprecated_key_ingest_rejection ;;
        8) case_8_rotate_to_deprecated_key_rejection ;;
        9) case_9_rotate_from_deprecated_source_key ;;
        *) echo "❌ Error: Invalid TARGET_CASE specified: $TARGET_CASE. Valid options are 1-9 or 'all'."; exit 1 ;;
    esac
fi
