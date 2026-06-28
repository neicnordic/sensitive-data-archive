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

    # This download request will hit downloadv2, which passes the rotated header block to reencrypt.
    # Because reencrypt still has rotatekey.sec.pem, this request will succeed.
    curl -s -H "Authorization: Bearer $TOKEN" -H "X-C4GH-Public-Key: $CLIENT_PUB_KEY" \
        http://localhost:8085/files/EGAF00000000101 -o "$OUTDIR"/c1-after.c4gh

    C4GH_PASSWORD=c4ghpass sda-cli decrypt --key "$SHARED_DIR"/client.sec.pem "$OUTDIR"/c1-after.c4gh
    echo "SUCCESS: User successfully decrypted the file generated after key rotation!"

    echo -e "\nStep 1.5: Verifying key removal safety: Attempting to download an unrotated file..."
    # If you have an unrotated file (e.g., EGAF00000000102), this call should fail with a 500 error
    STATUS_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
    -H "Authorization: Bearer $TOKEN" -H "X-C4GH-Public-Key: $CLIENT_PUB_KEY" \
    http://localhost:8085/files/EGAF00000000102 || true)

    if [ "$STATUS_CODE" -eq 500 ]; then
        echo "SUCCESS: Unrotated files are securely blocked because the old key was removed!"
    else
        echo "WARNING: Unexpected response code $STATUS_CODE"
    fi
}

# =================================================================================
mkdir -p "$SHARED_DIR"
# Copy shared folder from the container to the local shared directory for use in the demo
docker cp verify:/shared/ $SHARED_DIR/ 

TOKEN=$(curl -s http://localhost:8000/tokens | jq -r '.[0]')
CLIENT_PUB_KEY=$(base64 -w0 "$SHARED_DIR"/client.pub.pem)
API_HOST="http://localhost:8090"

# Run the demo script for the key rotation test
case_1_standard_lifecycle

pause_step
