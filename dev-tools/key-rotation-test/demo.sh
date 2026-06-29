#!/usr/bin/env bash
set -euo pipefail

# --- CONFIGURATION & ENV SETUP ---
export SHARED_DIR="/tmp/sda/shared"
export POSTGRES_IMAGE="postgres"
export DB_OPTS="-U postgres -d sda"
export PR_NUMBER=$(docker ps --format "{{.Image}}" | grep -oE "PR[0-9]{4}-[0-9]{2}-[0-9]{2}" | head -n 1 | sed 's/PR//')

# all files should be stored in a local temp directory to avoid polluting the shared folder
OUTDIR="/tmp/sda/test-data"
mkdir -p "$OUTDIR"

# delete any existing files in the output directory to ensure a clean slate for the demo
rm -f "$OUTDIR"/*

# ======================================================================
# Helper function to pause between phases
pause_step() {
    echo -e "\n\033[1;33m>>> PRESS [ENTER] TO PROCEED TO THE NEXT TEST CASE...\033[0m"
    read -r
}

log_header() {
    echo -e "\n\033[1;32m========================================================================\033[0m"
    echo -e "\033[1;32m$1\033[0m"
    echo -e "\033[1;32m========================================================================\033[0m"
}

# Creates a clean snapshot of the database state right after initialization
backup_database() {
    echo "Creating clean snapshot of initialized database..."
    docker compose exec -e PGPASSWORD=rootpasswd postgres pg_dump -U postgres -d sda -F c -b -v -f /var/lib/postgresql/data/clean_db.dump > /dev/null
    echo "Snapshot clean_db.dump successfully stored."
}

# Flashes the database back to pristine, original status
restore_database() {
    echo "Restoring database to clean snapshot state..."
    # Terminate active backend connections to allow drop/restore operations
    docker compose exec -e PGPASSWORD=rootpasswd postgres psql -U postgres -d sda -c \
        "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'sda' AND pid <> pg_backend_pid();" > /dev/null 2>&1

    # Clean and restore database structures
    docker compose exec -e PGPASSWORD=rootpasswd postgres dropdb -U postgres --if-exists sda
    docker compose exec -e PGPASSWORD=rootpasswd postgres createdb -U postgres sda
    docker compose exec -e PGPASSWORD=rootpasswd postgres pg_restore -U postgres -d sda /var/lib/postgresql/data/clean_db.dump > /dev/null
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
    FILE_ID=$(docker compose exec -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c "SELECT id FROM sda.files WHERE stable_id = 'EGAF00000000101';")
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
    FILE1_ID=$(docker compose exec -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c "SELECT id FROM sda.files WHERE stable_id = 'EGAF00000000101';")
    FILE2_ID=$(docker compose exec -e PGPASSWORD=rootpasswd postgres psql $DB_OPTS -tA -c "SELECT id FROM sda.files WHERE stable_id = 'EGAF00000000102';")

    echo "File 1 (To be rotated): ID=$FILE1_ID, StableID=EGAF00000000101"
    echo "File 2 (To be left alone): ID=$FILE2_ID, StableID=EGAF00000000102"

    echo -e "\nStep 2.3: Executing key rotation ONLY on File 1..."
    curl -s -H "Authorization: Bearer $TOKEN" -X POST "$API_HOST/file/rotatekey/$FILE1_ID"
    echo "Rotation command issued for File 1."

    echo -e "\nStep 2.4: Attempting download and decryption of the ROTATED file (File 1)..."
    curl -s -H "Authorization: Bearer $TOKEN" -H "X-C4GH-Public-Key: $CLIENT_PUB_KEY" \
        http://localhost:8085/files/EGAF00000000101 -o "$OUTDIR"/c2-file1-rotated.c4gh

    C4GH_PASSWORD=c4ghpass sda-cli decrypt --key "$SHARED_DIR"/client.sec.pem "$OUTDIR"/c2-file1-rotated.c4gh
    echo "SUCCESS: File 1 (new key) downloaded and decrypted perfectly!"

    echo -e "\nStep 2.5: Attempting download and decryption of the UNROTATED file (File 2)..."
    curl -s -H "Authorization: Bearer $TOKEN" -H "X-C4GH-Public-Key: $CLIENT_PUB_KEY" \
        http://localhost:8085/files/EGAF00000000102 -o "$OUTDIR"/c2-file2-legacy.c4gh

    C4GH_PASSWORD=c4ghpass sda-cli decrypt --key "$SHARED_DIR"/client.sec.pem "$OUTDIR"/c2-file2-legacy.c4gh
    echo "SUCCESS: File 2 (old key) downloaded and decrypted perfectly!"

    echo -e "\n\033[1;32mSUCCESS: Mixed-key dataset handles both active cryptographic keys simultaneously!\033[0m"
}

# =================================================================================
mkdir -p "$SHARED_DIR"
# Copy shared folder from the container to the local shared directory for use in the demo
docker cp verify:/shared/ $SHARED_DIR/ 

TOKEN=$(curl -s http://localhost:8000/tokens | jq -r '.[0]')
CLIENT_PUB_KEY=$(base64 -w0 "$SHARED_DIR"/client.pub.pem)
API_HOST="http://localhost:8090"

# Take a snapshot of the clean database state
backup_database

# Run the demo script for the key rotation test
case_1_standard_lifecycle

pause_step

case_2_mixed_key_dataset

pause_step
