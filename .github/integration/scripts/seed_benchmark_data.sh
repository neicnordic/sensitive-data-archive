#!/bin/bash
# Minimal benchmark data seeding script
# Only seeds the data needed for download benchmarking - no complex test assertions
set -e

cd /shared || true

echo "=== Benchmark Data Seeding ==="

# Install minimal dependencies
echo "[1/5] Installing dependencies..."
apt-get -o DPkg::Lock::Timeout=60 update > /dev/null
apt-get -o DPkg::Lock::Timeout=60 install -y curl jq postgresql-client xxd > /dev/null

pip install --upgrade pip > /dev/null
pip install s3cmd aiohttp Authlib joserfc requests > /dev/null

# Generate s3cfg with correct user (test@dummy.org)
echo "Generating s3cfg..."
cat >/shared/s3cfg <<EOD
[default]
access_key=test@dummy.org
secret_key=test@dummy.org
access_token="$(python /scripts/sign_jwt.py test@dummy.org)"
check_ssl_certificate = False
check_ssl_hostname = False
encoding = UTF-8
encrypt = False
guess_mime_type = True
host_base = s3inbox:8000
host_bucket = s3inbox:8000
human_readable_sizes = true
multipart_chunk_size_mb = 50
use_https = False
socket_timeout = 30
EOD

# Wait for services to be ready
echo "[2/5] Waiting for services..."
until psql -U postgres -h postgres -d sda -c "SELECT 1" > /dev/null 2>&1; do
    echo "  Waiting for postgres..."
    sleep 2
done

# Insert encryption keys (required for proper key hash tracking)
echo "Inserting encryption keys..."
C4GH_KEY_HASH=$(awk 'NR==2' /shared/c4gh.pub.pem | base64 -d | xxd -p -c256)
ROTATE_KEY_HASH=$(awk 'NR==2' /shared/rotatekey.pub.pem | base64 -d | xxd -p -c256)
psql -v ON_ERROR_STOP=1 -U postgres -h postgres -d sda -At -c "INSERT INTO sda.encryption_keys(key_hash, description) VALUES('$C4GH_KEY_HASH', 'test c4gh key') ON CONFLICT (key_hash) DO NOTHING;" > /dev/null
psql -v ON_ERROR_STOP=1 -U postgres -h postgres -d sda -At -c "INSERT INTO sda.encryption_keys(key_hash, description) VALUES('$ROTATE_KEY_HASH', 'test rotate key') ON CONFLICT (key_hash) DO NOTHING;" > /dev/null

until curl -s -u guest:guest http://rabbitmq:15672/api/overview > /dev/null 2>&1; do
    echo "  Waiting for rabbitmq..."
    sleep 2
done

# Download and encrypt test files
echo "[3/5] Preparing test files..."
for file in NA12878.bam NA12878_20k_b37.bam; do
    if [ ! -f "/shared/$file" ]; then
        curl --retry 10 -s -L -o "/shared/$file" \
            "https://github.com/ga4gh/htsget-refserver/raw/main/data/gcp/gatk-test-data/wgs_bam/$file"
    fi
    if [ ! -f "/shared/$file.c4gh" ]; then
        # Use simpler approach from 10_upload_test.sh: pipe yes to handle prompts
        # This uses ephemeral sender key (no -s argument) which is fine for test data
        yes | /shared/crypt4gh encrypt -p /shared/c4gh.pub.pem -f "/shared/$file"
    fi
done

# Upload files via S3 inbox
echo "[4/5] Uploading files..."
for file in NA12878.bam.c4gh NA12878_20k_b37.bam.c4gh; do
    s3cmd -c /shared/s3cfg put "/shared/$file" "s3://test_dummy.org/$file" --no-check-certificate 2>/dev/null || true
done

# Resolve the latest uploaded file rows directly from Postgres.
# The inbox queue is not safe to use for this because s3inbox emits multiple
# messages per upload, so FIFO consumption can pair the wrong file with the
# wrong correlation ID on repeated runs.
declare -A FILE_IDS
echo "  Resolving uploaded file IDs..."
for file in NA12878.bam.c4gh NA12878_20k_b37.bam.c4gh; do
    RETRY=0
    FILE_ID=""
    until [ -n "$FILE_ID" ]; do
        FILE_ID=$(psql -U postgres -h postgres -d sda -At -c "SELECT id FROM sda.files WHERE submission_user='test@dummy.org' AND submission_file_path='$file' ORDER BY created_at DESC LIMIT 1")
        if [ -n "$FILE_ID" ]; then
            FILE_IDS["$file"]="$FILE_ID"
            break
        fi
        echo "  Waiting for database registration for $file..."
        RETRY=$((RETRY + 1))
        if [ "$RETRY" -ge 60 ]; then
            echo "ERROR: Database registration timeout for $file"
            exit 1
        fi
        sleep 2
    done
done

# Manually trigger ingestion (bridge inbox -> ingest)
echo "[4.5/5] Triggering ingestion..."
for file in NA12878.bam.c4gh NA12878_20k_b37.bam.c4gh; do
    echo "  Triggering ingest for $file..."
    
    # Calculate checksums of encrypted file
    ENC_SHA=$(sha256sum "/shared/$file" | cut -d' ' -f 1)
    ENC_MD5=$(md5sum "/shared/$file" | cut -d' ' -f 1)

    CORRID="${FILE_IDS[$file]}"

    if [ -z "$CORRID" ]; then
        echo "ERROR: Failed to resolve file ID for $file"
        exit 1
    fi

    # Construct ingest message properties
    properties=$(jq -c -n \
        --argjson delivery_mode 2 \
        --arg correlation_id "$CORRID" \
        --arg content_encoding UTF-8 \
        --arg content_type application/json \
        '$ARGS.named')

    # Construct encrypted checksums JSON
    encrypted_checksums=$(jq -c -n \
        --arg sha256 "$ENC_SHA" \
        --arg md5 "$ENC_MD5" \
        '$ARGS.named|to_entries|map(with_entries(select(.key=="key").key="type"))')

    # Use correct file path for ingest payload (s3 bucket path)
    # Upload was to s3://test_dummy.org/$file
    # Ingest expects path relative to bucket? Or full path?
    # 20_ingest-verify_test.sh uses "test_dummy.org/NA12878.bam" (NO .c4gh extension??)
    # Wait, 20_ingest-verify_test.sh line 36: if file is NA12878.bam then file="test_dummy.org/NA12878.bam".
    # And line 43: --arg filepath "$file.c4gh"
    # So it becomes "test_dummy.org/NA12878.bam.c4gh".
    # My files are uploaded to "s3://test_dummy.org/$file".
    # So path should be "test_dummy.org/$file" (bucket/key).
    
    ingest_path="test_dummy.org/$file"

    ingest_payload=$(jq -r -c -n \
        --arg type ingest \
        --arg user test@dummy.org \
        --arg filepath "$ingest_path" \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64')

    ingest_body=$(jq -c -n \
        --arg vhost sda \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "ingest" \
        --arg payload_encoding base64 \
        --arg payload "$ingest_payload" \
        '$ARGS.named')

    curl -s -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
        -H 'Content-Type: application/json;charset=UTF-8' \
        -d "$ingest_body" > /dev/null
done

# Wait for ingest to complete (files become archived)
echo "[5/5] Waiting for ingestion pipeline..."
RETRY=0
until [ "$(psql -U postgres -h postgres -d sda -At -c "SELECT COUNT(*) FROM sda.files WHERE archive_file_path IS NOT NULL")" -ge 2 ]; do
    echo "  Waiting for ingest..."
    RETRY=$((RETRY + 1))
    if [ "$RETRY" -ge 120 ]; then
        echo "ERROR: Ingest timeout"
        exit 1
    fi
    sleep 2
done

# Wait for verification (checksums populated)
echo "  Waiting for verification..."
RETRY=0
until [ "$(psql -U postgres -h postgres -d sda -At -c "SELECT COUNT(*) FROM sda.checksums WHERE source='UNENCRYPTED' AND type='SHA256'")" -ge 2 ]; do
    echo "  Waiting for verification..."
    RETRY=$((RETRY + 1))
    if [ "$RETRY" -ge 120 ]; then
        echo "ERROR: Verification timeout"
        exit 1
    fi
    sleep 2
done

# Wait for verified messages to be published before consuming them.
# The DB checksums can appear slightly before the verify worker has pushed
# messages into RabbitMQ, which caused a race in the benchmark seeding flow.
echo "  Waiting for verified queue..."
RETRY=0
until [ "$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/verified | jq -r '.messages_ready')" -ge 2 ]; do
    echo "  Waiting for verified queue..."
    RETRY=$((RETRY + 1))
    if [ "$RETRY" -ge 120 ]; then
        echo "ERROR: Verified queue timeout"
        curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/verified | jq .
        exit 1
    fi
    sleep 2
done

# Trigger finalize via MQ (receive verified message, send accession message)
echo "Sending accession messages..."

# We expect 2 verified messages
for i in 1 2; do
    # Get verified message to chain correlation ID and get checksums
    MSG=$(curl -s -X POST \
            -H "content-type:application/json" \
            -u guest:guest http://rabbitmq:15672/api/queues/sda/verified/get \
            -d '{"count":1,"encoding":"auto","ackmode":"ack_requeue_false"}')
    
    # Check if we got a valid message
    if [ "$(echo "$MSG" | jq -r 'length')" -eq 0 ]; then
        echo "ERROR: Expected verified message not found"
        exit 1
    fi

    corrid=$(jq -r '.[0].properties.correlation_id // ""' <<< "$MSG")
    user=$(jq -r '.[0].payload|fromjson|.user' <<< "$MSG")
    filepath=$(jq -r '.[0].payload|fromjson|.filepath' <<< "$MSG")
    # verification service returns extracted checksums in the payload
    # verify payload format: {"type":"verified", "user":"...", "filepath":"...", "decrypted_checksums": [...]}
    # We need to pass this array as-is to the accession message
    decrypted_checksums=$(jq -r '.[0].payload|fromjson|.decrypted_checksums' <<< "$MSG")
    
    # Construct properties with chained correlation ID
    properties=$(jq -c -n \
        --argjson delivery_mode 2 \
        --arg correlation_id "$corrid" \
        --arg content_encoding UTF-8 \
        --arg content_type application/json \
        '$ARGS.named')
    
    # Assign accession ID (sequential)
    accession_id="EGAF7490000000$i"
    
    payload=$(jq -r -c -n \
        --arg type accession \
        --arg user "$user" \
        --arg filepath "$filepath" \
        --arg accession_id "$accession_id" \
        --argjson decrypted_checksums "$decrypted_checksums" \
        '$ARGS.named|@base64')
    
    body=$(jq -c -n \
        --arg vhost sda \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key accession \
        --arg payload_encoding base64 \
        --arg payload "$payload" \
        '$ARGS.named')
    
    echo "  Sending accession for $filepath (accession: $accession_id)..."
    curl -s -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
        -H 'Content-Type: application/json' -d "$body" > /dev/null
done

# Wait for finalize
RETRY=0
until [ "$(psql -U postgres -h postgres -d sda -At -c "SELECT COUNT(*) FROM sda.files WHERE stable_id IS NOT NULL")" -ge 2 ]; do
    echo "  Waiting for finalize..."
    RETRY=$((RETRY + 1))
    if [ "$RETRY" -ge 60 ]; then
        echo "ERROR: Finalize timeout"
        exit 1
    fi
    sleep 2
done

# Create dataset mapping
echo "Creating dataset mapping..."
mappings=$(jq -c -n '$ARGS.positional' --args "EGAF74900000001" --args "EGAF74900000002")
mapping_payload=$(jq -r -c -n \
    --arg type mapping \
    --arg dataset_id BENCHMARK-DATASET-001 \
    --argjson accession_ids "$mappings" \
    '$ARGS.named|@base64')

mapping_body=$(jq -c -n \
    --arg vhost test \
    --arg name sda \
    --argjson properties "$properties" \
    --arg routing_key mappings \
    --arg payload_encoding base64 \
    --arg payload "$mapping_payload" \
    '$ARGS.named')

curl -s -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json' -d "$mapping_body" > /dev/null

# Wait for mapping
RETRY=0
until [ "$(psql -U postgres -h postgres -d sda -At -c "SELECT COUNT(*) FROM sda.file_dataset")" -ge 2 ]; do
    echo "  Waiting for mapping..."
    RETRY=$((RETRY + 1))
    if [ "$RETRY" -ge 30 ]; then
        echo "ERROR: Mapping timeout"
        exit 1
    fi
    sleep 2
done

# Release dataset
release_payload=$(jq -r -c -n --arg type release --arg dataset_id BENCHMARK-DATASET-001 '$ARGS.named')
release_body=$(jq -c -n \
    --arg vhost test \
    --arg name sda \
    --argjson properties "$properties" \
    --arg routing_key mappings \
    --arg payload_encoding string \
    --arg payload "$release_payload" \
    '$ARGS.named')

curl -s -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json' -d "$release_body" > /dev/null

# Wait for release
RETRY=0
until [ "$(psql -U postgres -h postgres -d sda -At -c "SELECT event FROM sda.dataset_event_log WHERE dataset_id='BENCHMARK-DATASET-001' ORDER BY event_date DESC LIMIT 1")" = "released" ]; do
    echo "  Waiting for release..."
    RETRY=$((RETRY + 1))
    if [ "$RETRY" -ge 30 ]; then
        echo "WARNING: Release might not have completed, continuing anyway"
        break
    fi
    sleep 2
done

echo ""
echo "=== Benchmark Data Ready ==="
echo "Dataset: BENCHMARK-DATASET-001"
echo "Files: EGAF74900000001, EGAF74900000002"
echo ""
