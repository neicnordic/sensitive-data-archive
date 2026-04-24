#!/usr/bin/env bash
# Local k3d smoke test for the rotatekey service chart.
#
# Prerequisites: docker, k3d, kubectl, helm
#
# Usage:
#   ./dev-tools/chart-smoke-test/test-rotatekey.sh               # full run (build + deploy + test)
#   ./dev-tools/chart-smoke-test/test-rotatekey.sh --no-build    # skip image build (reuse existing)
#   ./dev-tools/chart-smoke-test/test-rotatekey.sh --cleanup     # tear down cluster and exit

set -euo pipefail

CLUSTER_NAME="sda-rotatekey-test"
IMAGE="ghcr.io/neicnordic/sensitive-data-archive:local-test"
SKIP_BUILD=false

for arg in "$@"; do
    case "$arg" in
        --no-build) SKIP_BUILD=true ;;
        --cleanup)
            echo "Tearing down cluster $CLUSTER_NAME..."
            k3d cluster delete "$CLUSTER_NAME" 2>/dev/null || true
            echo "Done."
            exit 0
            ;;
        *) echo "Unknown argument: $arg"; exit 1 ;;
    esac
done

# -- helpers --
pass=0
fail=0

wait_for_pod() { # wait for a pod with the given label to be ready, with a timeout
    local label="$1" timeout="$2"
    echo "Waiting for pod $label to be ready (${timeout}s)..."
    if ! kubectl wait --for=condition=ready pod -l "$label" --timeout="${timeout}s" 2>/dev/null; then
        echo "Pod $label not ready after ${timeout}s"
        kubectl logs -l "$label" --tail=20 2>/dev/null
        return 1
    fi
}

echo "=== Step 1: k3d cluster ==="
# Pre-create a dedicated network with the correct MTU (1400) to avoid TLS timeouts
if ! docker network ls | grep -q "sda-test-net"; then
    echo "Creating custom Docker network with MTU 1400..."
    docker network create sda-test-net --opt com.docker.network.driver.mtu=1400
fi

if k3d cluster list 2>/dev/null | grep -q "$CLUSTER_NAME"; then
    echo "Cluster $CLUSTER_NAME already exists, reusing."
else
    echo "Creating cluster $CLUSTER_NAME..."
    # Tell k3d to use our custom network instead of generating its own
    k3d cluster create "$CLUSTER_NAME" --network sda-test-net --wait
fi

echo "=== Step 2: image ==="
if [ "$SKIP_BUILD" = true ]; then
    echo "Skipping build (--no-build)."
else
    echo "Building sda image..."
    docker build -t "$IMAGE" --build-arg GOLANG_VERSION=1.25 --network=host -f sda/Dockerfile sda/
fi

echo "Cleaning old image from k3d and importing fresh..."
k3d image delete "$IMAGE" -c "$CLUSTER_NAME" 2>/dev/null || true

echo "Importing image into cluster..."
k3d image import "$IMAGE" -c "$CLUSTER_NAME"

echo "=== Step 3: dependencies ==="

if ! helm status postgres >/dev/null 2>&1; then
    echo "Installing postgres..."
    helm install postgres charts/sda-db \
        --set global.postgresAdminPassword=rootpass \
        --set global.tls.enabled=false \
        --set persistence.enabled=false \
        --set resources=null \
        --wait --timeout 120s
else
    echo "Postgres already installed."
fi

if ! helm status broker >/dev/null 2>&1; then
    echo "Installing rabbitmq..."
    helm install broker charts/sda-mq \
        --set global.adminPassword=mqpass \
        --set global.adminUser=admin \
        --set global.tls.enabled=false \
        --set persistence.enabled=false \
        --set resources=null \
        --wait --timeout 120s
else
    echo "RabbitMQ already installed."
fi

echo "=== Step 4: keys and secrets ==="

# Clean previous run if exists
kubectl delete deploy pipeline-sda-svc-rotatekey 2>/dev/null || true
kubectl delete secret pipeline-sda-svc-rotatekey 2>/dev/null || true

# OLD_PUB matches the "old" identity
OLD_PUB="-----BEGIN CRYPT4GH PUBLIC KEY-----
miSC7pgHDY3BGLajuGjNz0K3+6TSJ7wEFGpNATZ1DV4=
-----END CRYPT4GH PUBLIC KEY-----"

# ROTATE_PUB is the "new" destination key
ROTATE_PUB="-----BEGIN CRYPT4GH PUBLIC KEY-----
fFmwrVXywijqMoaLX95CgIXp6klJuo5MOLf/I3+BQ1Q=
-----END CRYPT4GH PUBLIC KEY-----"

ROTATE_PUB_BASE64=$(echo "$ROTATE_PUB" | base64 -w0)

# OLD_PRIV is the encrypted private key corresponding to OLD_PUB, with passphrase "pass"
OLD_PRIV="-----BEGIN CRYPT4GH ENCRYPTED PRIVATE KEY-----
YzRnaC12MQAGc2NyeXB0ABQAAAAA9yimIi6tLdrRZim6rlojxQARY2hhY2hhMjBf
cG9seTEzMDUAPPe+btMB/qNtpH1jxIuLonMlOSyrLBFtr7A/9QNyalr33zSA24OB
Eo9Y+poiXK0ECZdEPOPIOvTWewuzSQ==
-----END CRYPT4GH ENCRYPTED PRIVATE KEY-----"

# Create the C4GH Secret for the reencrypt service
kubectl create secret generic c4gh-secret \
  --namespace default \
  --from-literal=c4gh.key="$OLD_PRIV" \
  --dry-run=client -o yaml | kubectl apply -f -

OLD_HEX=$(echo "$OLD_PUB" | awk 'NR==2' | base64 -d | xxd -p -c256 | tr -d '\n\r ')
ROTATE_HEX=$(echo "$ROTATE_PUB" | awk 'NR==2' | base64 -d | xxd -p -c256 | tr -d '\n\r ')

echo "Registering key Hashes in sda.encryption_keys..."

kubectl exec -i postgres-sda-db-0 -- env PGPASSWORD=rootpass psql -U postgres -d sda <<EOF
DELETE FROM sda.encryption_keys;
INSERT INTO sda.encryption_keys (key_hash, description) VALUES ('$OLD_HEX', 'this is the c4gh key');
INSERT INTO sda.encryption_keys (key_hash, description) VALUES ('$ROTATE_HEX', 'this is the rotatekey key');
EOF

HELM_ARGS=(
    # --- basic image/service settings ---
    --set image.tag=local-test
    --set image.pullPolicy=IfNotPresent
    --set global.tls.enabled=false
    --set global.rbacEnabled=false

    # rotatekey
    --set "global.c4gh.rotatePubKeyData=$ROTATE_PUB_BASE64"

    # --- database ---
    --set global.db.host=postgres-sda-db
    --set global.db.user=postgres
    --set global.db.password=rootpass
    --set global.db.name=sda
    --set global.db.port=5432

    # --- broker ---
    --set global.broker.host=broker-sda-mq
    --set global.broker.port=5672
    --set global.broker.username=admin
    --set global.broker.password=mqpass
    --set global.broker.rotateKeyQueue=rotatekey
    --set global.broker.prefetchCount=2

    # --- reencrypt  ---
    --set global.reencrypt.host=pipeline-sda-svc-reencrypt
    --set global.reencrypt.port=50051
    --set reencrypt.readinessProbe.grpc.port=50051

    # --- security ---
    --set global.api.rbacFileSecret=rbac

    # --- c4gh for reencrypt  ---
    --set global.c4gh.secretName=c4gh-secret
    --set "global.c4gh.privateKeys[0].keyName=c4gh.key"
    --set "global.c4gh.privateKeys[0].passphrase=pass"

    # --- ingress ---
    --set global.ingress.deploy=false

    # --- logging ---
    --set global.log.level=debug
    --set global.log.format=json
)


echo "=== Step 5: deploy rotatekey ==="
for tmpl in rotatekey-secrets rotatekey-deploy reencrypt-secrets re-encrypt-deploy re-encrypt-service; do
    helm template pipeline charts/sda-svc "${HELM_ARGS[@]}" \
        --show-only "templates/${tmpl}.yaml"
done | kubectl apply -f -

wait_for_pod "role=rotatekey" 60

echo "=== Step 6: check if the rotatekey service is up ==="
READY=$(kubectl get pod -l role=rotatekey -o jsonpath='{.items[0].status.containerStatuses[0].ready}')

if [ "$READY" = "true" ]; then
    echo "Rotatekey is up and connected."
else
    echo "Rotatekey is not ready."
    exit 1
fi

echo "=== Step 7: Injecting Test Data and Triggering Rotation ==="
# The dummy header is generated by encrypting a text file contains a char '1' by
# $OLD_PUB, and then extracting the header part.
DUMMY_HEADER="637279707434676801000000010000006c00000000000000ba20f3298e6439bc4f275465da9fe6eabd24f792e32ed0cb13c8fe6e6df9e60cbc2f298bda69bb28500d7401ab50f1a4ec60959df3e92153f972b7dfdf5689b0e2a1e74ce30cc00087d1e03711db5788fbfae20345443272e61c5141e2673886e90efe19"

# Dummy SHA256 hashes
DUMMY_CHECKSUM_UPLOADED="ef0497ad9295e6c2f8c0e0e0d80252f78ab9c8e5e780236fb667463aa1d27966"
DUMMY_CHECKSUM_ARCHIVED="14fcf26fb20ad63579e7283a0a590ca662a091ec089ff8ada2a69c1d5ff1cd5c"
DUMMY_CHECKSUM_UNENCRYPTED="5d2ade2208b357b3abcda5188217fcfa286369573cff03052510f6904dfa5d02"

# Create a dummy file record (with header!)
FILE_ID=$(cat /proc/sys/kernel/random/uuid)
echo "Generated File ID: $FILE_ID"

kubectl exec -i postgres-sda-db-0 -- env PGPASSWORD=rootpass psql -U postgres -d sda <<EOF
-- Insert the file record with a non-null header
INSERT INTO sda.files (
    id, 
    submission_user, 
    submission_file_path, 
    archive_file_path, 
    decrypted_file_size, 
    key_hash,
    header,
    encryption_method
) VALUES (
    '$FILE_ID', 
    'test-user', 
    'dataset_rotatekey/testfile1.c4gh', 
    'archive/testfile1.c4gh', 
    1048576, 
    '$OLD_HEX',
    '$DUMMY_HEADER',
    'CRYPT4GH'
);

-- Register the 'verified' event
INSERT INTO sda.file_event_log (file_id, event, user_id, success)
VALUES ('$FILE_ID', 'verified', 'smoke-test-script', true);

-- Insert a checksum record
INSERT INTO sda.checksums (file_id, checksum, type, source) VALUES
('$FILE_ID', '$DUMMY_CHECKSUM_UPLOADED', 'SHA256', 'UPLOADED'),
('$FILE_ID', '$DUMMY_CHECKSUM_ARCHIVED', 'SHA256', 'ARCHIVED'),
('$FILE_ID', '$DUMMY_CHECKSUM_UNENCRYPTED', 'SHA256', 'UNENCRYPTED');
EOF

# Trigger the rotation via RabbitMQ
echo "Publishing rotation message to RabbitMQ..."
kubectl run mq-trigger --rm -i --restart=Never --image=curlimages/curl -- \
  curl -s -u admin:mqpass -X POST "http://broker-sda-mq:15672/api/exchanges/sda/sda/publish" \
  -H "content-type:application/json" \
  -d '{
    "properties": {"delivery_mode": 2},
    "routing_key": "rotatekey",
    "payload": "{\"type\":\"key_rotation\",\"file_id\":\"'"$FILE_ID"'\"}",
    "payload_encoding": "string"
  }'

echo "Waiting for services to react..."
sleep 5


echo "=== Step 8: Verification ==="
fail=0
pass=0
# Capture the current hash for the file
CURRENT_HASH=$(kubectl exec -i postgres-sda-db-0 -- env PGPASSWORD=rootpass psql -U postgres -d sda -At -c "SELECT key_hash FROM sda.files WHERE id='$FILE_ID';")

echo "Database Hash for $FILE_ID: $CURRENT_HASH"

if [ "$CURRENT_HASH" == "$ROTATE_HEX" ]; then
    echo "  PASS: Database record updated to the new key hash."
    pass=$((pass + 1))
elif [ -z "$CURRENT_HASH" ]; then
    echo "  FAIL: Database record not found or Hash is empty."
    fail=$((fail + 1))
else
    echo "  FAIL: Database record still has hash $CURRENT_HASH (expected $ROTATE_HEX)."
    fail=$((fail + 1))
fi

# Check the logs of the rotatekey pod for evidence of successful processing
ROTATE_LOGS=$(kubectl logs -l role=rotatekey --tail=50)

if echo "$ROTATE_LOGS" | grep -q "Successfully set header and key hash"; then
    echo "  PASS: Key rotation logic completed (Reencrypt + DB Update)."
    pass=$((pass + 1))
else
    echo "  FAIL: Key rotation did not reach completion."
    fail=$((fail + 1))
fi

# Peek into the 'archived' queue to see if rotatekey sent the message
VALIDATION_QUEUE="archived"
MQ_RESPONSE=$(kubectl run mq-trigger --rm -i --restart=Never --image=curlimages/curl -- \
  curl -s -u admin:mqpass \
  -X POST "http://broker-sda-mq:15672/api/queues/sda/${VALIDATION_QUEUE}/get" \
  -H "content-type:application/json" \
  -d '{"count":1,"ackmode":"ack_requeue_true","encoding":"auto","truncate":50000}')

if echo "$MQ_RESPONSE" | grep -q "$FILE_ID"; then
    echo "  PASS: Outgoing verification message for $FILE_ID found in '${VALIDATION_QUEUE}' queue."
    pass=$((pass + 1))
else
    echo "  FAIL: No message found for $FILE_ID in '${VALIDATION_QUEUE}' queue."
    echo "  Hint: Check if rotatekey logs show 'Published message to queue'"
    fail=$((fail + 1))
fi

echo "=== Summary: $pass passed, $fail failed ==="
if [ $fail -gt 0 ]; then
    exit 1
fi
echo "Rotatekey Smoke Test Completed Successfully!"