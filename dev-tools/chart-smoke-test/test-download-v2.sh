#!/usr/bin/env bash
# Local k3d smoke test for the v2 download service chart.
#
# Prerequisites: docker, k3d, kubectl, helm, yq (mikefarah), jq
#
# Usage:
#   ./dev-tools/chart-smoke-test/test-download-v2.sh              # full run (build + deploy + test)
#   ./dev-tools/chart-smoke-test/test-download-v2.sh --no-build    # skip image build (reuse existing)
#   ./dev-tools/chart-smoke-test/test-download-v2.sh --cleanup      # tear down cluster and exit
set -euo pipefail

CLUSTER_NAME="sda-download-v2-test"
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
check() {
    local name="$1" expected="$2" actual="$3"
    if echo "$actual" | grep -q "$expected" 2>/dev/null; then
        echo "  PASS  $name"
        pass=$((pass + 1))
    else
        echo "  FAIL  $name (expected '$expected', got '$actual')"
        fail=$((fail + 1))
    fi
}

wait_for_pod() {
    local label="$1" timeout="$2"
    echo "Waiting for pod $label to be ready (${timeout}s)..."
    if ! kubectl wait --for=condition=ready pod -l "$label" --timeout="${timeout}s" 2>/dev/null; then
        echo "Pod $label not ready after ${timeout}s"
        kubectl logs -l "$label" --tail=20 2>/dev/null
        return 1
    fi
}

wait_for_deploy() {
    local name="$1" timeout="$2"
    echo "Waiting for deployment $name to be Available (${timeout}s)..."
    if ! kubectl rollout status "deploy/$name" --timeout="${timeout}s"; then
        echo "Deployment $name not Available after ${timeout}s"
        kubectl logs "deploy/$name" --tail=30 2>/dev/null
        return 1
    fi
}

# Render the v2 templates across config combinations (TLS, storage, ingress,
# networkPolicy) without deploying — catches regressions in templates that the
# single-slice deploy test doesn't exercise (ingress, certificate, netpol).
render_matrix() {
    echo "=== Template render matrix ==="
    local matrix_pass=0 matrix_fail=0
    local failed=()

    local base=(
        --set global.deploymentType=external
        --set global.download.enabled=false
        --set global.doa.enabled=false
        --set global.db.host=db --set global.db.user=u --set global.db.password=p
        --set global.broker.host=mq --set global.broker.username=u --set global.broker.password=p
        --set global.archive.s3Url=http://s3
        --set global.archive.s3AccessKey=a --set global.archive.s3SecretKey=s
        --set global.reencrypt.host=r
        --set global.api.jwtSecret=s --set global.api.rbacFileSecret=r
        --set global.oidc.id=id --set global.oidc.secret=s --set global.oidc.provider=http://oidc
        --set global.c4gh.secretName=c --set global.c4gh.publicKey=p
        --set "global.c4gh.privateKeys[0].keyName=k"
        --set "global.c4gh.privateKeys[0].passphrase=p"
        --set "global.c4gh.privateKeys[0].keyData=d"
        --set global.ingress.hostName.api=api.t
        --set global.ingress.hostName.auth=auth.t
        --set global.ingress.hostName.download=download.t
        --set global.ingress.hostName.syncapi=syncapi.t
        --set global.ingress.hostName.downloadV2=dl-v2.t
        --set global.downloadV2.enabled=true
        --set global.downloadV2.service.orgName=TestOrg
        --set global.downloadV2.service.orgURL=http://t
        --set downloadV2.replicaCount=1
    )

    for tls in false true; do
        for storage in s3 posix; do
            for ingress in false true; do
                for netpol in false true; do
                    local desc="tls=$tls storage=$storage ingress=$ingress netpol=$netpol"
                    local args=("${base[@]}"
                        --set "global.tls.enabled=$tls"
                        --set "global.archive.storageType=$storage"
                        --set "global.ingress.deploy=$ingress"
                        --set "global.networkPolicy.create=$netpol"
                    )
                    if [ "$tls" = "true" ]; then
                        args+=(--set global.tls.issuer=test-issuer)
                    fi
                    if [ "$storage" = "posix" ]; then
                        args+=(--set global.archive.existingClaim=archive-pvc)
                    fi
                    if helm template test charts/sda-svc "${args[@]}" >/dev/null 2>&1; then
                        echo "  PASS  $desc"
                        matrix_pass=$((matrix_pass + 1))
                    else
                        echo "  FAIL  $desc"
                        matrix_fail=$((matrix_fail + 1))
                        failed+=("$desc")
                    fi
                done
            done
        done
    done

    echo "  Matrix: $matrix_pass passed, $matrix_fail failed"
    if [ "$matrix_fail" -gt 0 ]; then
        echo "  Failing combinations:"
        for c in "${failed[@]}"; do
            echo "    - $c"
            helm template test charts/sda-svc "${base[@]}" \
                --set "global.tls.enabled=$(echo "$c" | sed -n 's/.*tls=\([^ ]*\).*/\1/p')" \
                --set "global.archive.storageType=$(echo "$c" | sed -n 's/.*storage=\([^ ]*\).*/\1/p')" \
                --set "global.ingress.deploy=$(echo "$c" | sed -n 's/.*ingress=\([^ ]*\).*/\1/p')" \
                --set "global.networkPolicy.create=$(echo "$c" | sed -n 's/.*netpol=\([^ ]*\).*/\1/p')" \
                2>&1 | tail -2 | sed 's/^/      /'
        done
        return 1
    fi
    return 0
}

# -- 0. render matrix (fast fail) --
if ! render_matrix; then
    echo "Template matrix failed — aborting before any cluster work"
    exit 1
fi

# -- 1. cluster --
echo "=== Step 1: k3d cluster ==="
if k3d cluster list 2>/dev/null | grep -q "$CLUSTER_NAME"; then
    echo "Cluster $CLUSTER_NAME already exists, reusing."
else
    echo "Creating cluster $CLUSTER_NAME..."
    k3d cluster create "$CLUSTER_NAME" --wait
fi

# -- 2. build + import image --
echo "=== Step 2: image ==="
if [ "$SKIP_BUILD" = true ]; then
    echo "Skipping build (--no-build)."
else
    echo "Building sda image..."
    docker build -t "$IMAGE" --build-arg GOLANG_VERSION=1.25 --network=host -f sda/Dockerfile sda/
fi
echo "Importing image into cluster..."
k3d image import "$IMAGE" -c "$CLUSTER_NAME"

# -- 3. dependencies (postgres, rabbitmq, minio) --
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

if ! kubectl get deploy minio >/dev/null 2>&1; then
    echo "Installing minio..."
    kubectl apply -f - <<'MINIO_EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: minio
spec:
  replicas: 1
  selector:
    matchLabels:
      app: minio
  template:
    metadata:
      labels:
        app: minio
    spec:
      containers:
      - name: minio
        image: minio/minio:latest
        command: ["minio", "server", "/data"]
        env:
        - name: MINIO_ROOT_USER
          value: access
        - name: MINIO_ROOT_PASSWORD
          value: secretkey
        ports:
        - containerPort: 9000
---
apiVersion: v1
kind: Service
metadata:
  name: minio
spec:
  ports:
  - port: 443
    targetPort: 9000
  selector:
    app: minio
MINIO_EOF
    wait_for_pod "app=minio" 60
else
    echo "Minio already installed."
fi

# -- 4. service account --
kubectl create serviceaccount pipeline 2>/dev/null || true

# -- 5. deploy download-v2 --
echo "=== Step 4: deploy download-v2 ==="

# Clean previous run
kubectl delete deploy pipeline-sda-svc-download-v2 2>/dev/null || true
kubectl delete secret pipeline-sda-svc-download-v2 2>/dev/null || true
kubectl delete svc pipeline-sda-svc-download-v2 2>/dev/null || true

HELM_ARGS=(
    --set image.tag=local-test
    --set image.pullPolicy=IfNotPresent
    --set global.tls.enabled=false
    --set global.db.host=postgres-sda-db
    --set global.db.user=postgres
    --set global.db.password=rootpass
    --set global.broker.host=broker-sda-mq
    --set global.broker.username=admin
    --set global.broker.password=mqpass
    --set global.archive.storageType=s3
    --set global.archive.s3Url=http://minio
    --set global.archive.s3Port=443
    --set global.archive.s3AccessKey=access
    --set global.archive.s3SecretKey=secretkey
    --set global.reencrypt.host=pipeline-sda-svc-reencrypt
    --set global.ingress.deploy=false
    --set global.oidc.id=test
    --set global.oidc.secret=test
    --set global.oidc.provider=http://oidc
    --set global.api.jwtSecret=jwt-secret
    --set global.api.rbacFileSecret=rbac
    --set global.c4gh.secretName=c4gh-secret
    --set global.c4gh.publicKey=dummy
    --set "global.c4gh.privateKeys[0].keyName=c4gh.key"
    --set "global.c4gh.privateKeys[0].passphrase=pass"
    --set "global.c4gh.privateKeys[0].keyData=dummykey"
    --set global.downloadV2.enabled=true
    --set global.downloadV2.service.orgName=TestOrg
    --set global.downloadV2.service.orgURL=http://test.org
    # trustedIssuers set to exercise the iss.json secret (visa.enabled stays false
    # so the pod doesn't need a reachable OIDC endpoint).
    --set "global.downloadV2.visa.trustedIssuers[0].iss=https://example.com/oidc"
    --set "global.downloadV2.visa.trustedIssuers[0].jku=https://example.com/jwks"
    --set global.ingress.hostName.downloadV2=dl-v2.local
    # Single-node k3d can't satisfy topology spread for the default 2 replicas
    --set downloadV2.replicaCount=1
)

# Render only v2 templates and apply directly
for tmpl in download-v2-secrets download-v2-deploy download-v2-service; do
    helm template pipeline charts/sda-svc "${HELM_ARGS[@]}" \
        --show-only "templates/${tmpl}.yaml"
done | kubectl apply -f -

wait_for_deploy "pipeline-sda-svc-download-v2" 120

# -- 6a. secret content tests --
echo "=== Step 5a: secret content tests ==="

SECRET="pipeline-sda-svc-download-v2"
ISS_SECRET="pipeline-sda-svc-download-v2-iss"

CONFIG_YAML=$(kubectl get secret "$SECRET" -o jsonpath='{.data.config\.yaml}' | base64 -d)

check "config.yaml .service.org-name"  "TestOrg" \
    "$(echo "$CONFIG_YAML" | yq '.service.org-name')"
check "config.yaml .service.org-url"   "http://test.org" \
    "$(echo "$CONFIG_YAML" | yq '.service.org-url')"
check "config.yaml .api.port"          "8080" \
    "$(echo "$CONFIG_YAML" | yq '.api.port')"
check "config.yaml .storage.archive.s3[0].endpoint" "http://minio:443" \
    "$(echo "$CONFIG_YAML" | yq '.storage.archive.s3[0].endpoint')"
check "config.yaml .db.host"           "postgres-sda-db" \
    "$(echo "$CONFIG_YAML" | yq '.db.host')"

ISS_JSON=$(kubectl get secret "$ISS_SECRET" -o jsonpath='{.data.iss\.json}' | base64 -d)
check "iss.json is valid JSON"         "parsed" \
    "$(echo "$ISS_JSON" | jq -e . >/dev/null 2>&1 && echo parsed || echo invalid)"
check "iss.json [0].iss"               "https://example.com/oidc" \
    "$(echo "$ISS_JSON" | jq -r '.[0].iss')"
check "iss.json [0].jku"               "https://example.com/jwks" \
    "$(echo "$ISS_JSON" | jq -r '.[0].jku')"

# -- 6b. HTTP smoke tests --
echo "=== Step 5b: HTTP smoke tests ==="

SVC="pipeline-sda-svc-download-v2"

run_curl() {
    local name="$1"; shift
    kubectl run "$name" --rm -i --restart=Never --image=curlimages/curl \
        --quiet -- "$@" 2>/dev/null | grep -v '^pod .* deleted'
}

health=$(run_curl curl-health curl -sf "http://$SVC/health/ready")
check "/health/ready returns ok" '"status":"ok"' "$health"
check "/health/ready database ok" '"database":"ok"' "$health"
check "/health/ready storage ok" '"storage":"ok"' "$health"

svcinfo=$(run_curl curl-svcinfo curl -sf "http://$SVC/service-info")
check "/service-info returns org" '"name":"TestOrg"' "$svcinfo"
check "/service-info returns version" '"version":"2.0.0"' "$svcinfo"

authresp=$(run_curl curl-auth curl -s -o /dev/null -w '%{http_code}' "http://$SVC/datasets")
check "/datasets returns 401 without token" "401" "$authresp"

# -- summary --
echo ""
echo "=== Results: $pass passed, $fail failed ==="
if [ "$fail" -gt 0 ]; then
    echo "Pod logs:"
    kubectl logs -l app=pipeline-sda-svc-download-v2 --tail=30
    exit 1
fi
echo ""
echo "Cleanup: $0 --cleanup"
