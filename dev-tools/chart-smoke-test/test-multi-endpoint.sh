#!/usr/bin/env bash
# Multi-endpoint storage template render scenarios for sda-svc chart.
# Renders four representative scenarios end-to-end via `helm template`
# and asserts via yq on rendered service config + k8s manifest fields.
set -euo pipefail
ROOT="$(git rev-parse --show-toplevel)"; cd "$ROOT"

pass=0; fail=0
check() {
    local name="$1" expected="$2" actual="$3"
    if [ "$actual" = "$expected" ]; then echo "  PASS  $name"; pass=$((pass+1))
    else echo "  FAIL  $name (expected '$expected', got '$actual')"; fail=$((fail+1)); fi
}

# Match the MINIMAL set used by test-helpers.sh.
MINIMAL=(
  --set global.deploymentType=internal
  --set global.tls.enabled=false
  --set global.db.host=db --set global.db.user=u --set global.db.password=p
  --set global.broker.host=mq --set global.broker.username=u --set global.broker.password=p
  --set global.c4gh.secretName=c --set global.c4gh.publicKey=p
  --set 'global.c4gh.privateKeys[0].keyName=k'
  --set 'global.c4gh.privateKeys[0].passphrase=p'
  --set 'global.c4gh.privateKeys[0].keyData=d'
  --set global.cega.host=h --set global.cega.user=u --set global.cega.password=p
  --set global.ingress.deploy=false
  --set 'global.archive.s3[0].endpoint=http://default-archive'
  --set 'global.archive.s3[0].accessKey=k' --set 'global.archive.s3[0].secretKey=s'
  --set 'global.archive.s3[0].bucketPrefix=default-archive'
  --set 'global.inbox.s3[0].endpoint=http://default-inbox'
  --set 'global.inbox.s3[0].accessKey=k' --set 'global.inbox.s3[0].secretKey=s'
  --set 'global.inbox.s3[0].bucketPrefix=default-inbox'
  --set 'global.s3Inbox.url=http://default-s3inbox'
  --set 'global.s3Inbox.accessKey=k' --set 'global.s3Inbox.secretKey=s'
  --set 'global.s3Inbox.bucket=default-s3inbox'
)

echo "=== 1: archive 2× S3 (sharding by bucket-prefix) ==="
out=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.archive.s3[0].endpoint=http://s3' \
  --set 'global.archive.s3[0].accessKey=k1' --set 'global.archive.s3[0].secretKey=s1' \
  --set 'global.archive.s3[0].bucketPrefix=archive-a' --set 'global.archive.s3[0].maxBuckets=5' \
  --set 'global.archive.s3[1].endpoint=http://s3' \
  --set 'global.archive.s3[1].accessKey=k2' --set 'global.archive.s3[1].secretKey=s2' \
  --set 'global.archive.s3[1].bucketPrefix=archive-b' --set 'global.archive.s3[1].maxBuckets=5' \
  --show-only templates/finalize-secrets.yaml \
  2>/dev/null | yq '.stringData."config.yaml"')
check "1 archive entry count"    "2"         "$(echo "$out" | yq '.storage.archive.s3 | length')"
check "1 entry 0 max_buckets"    "5"         "$(echo "$out" | yq '.storage.archive.s3[0].max_buckets')"
check "1 entry 1 bucket_prefix"  "archive-b" "$(echo "$out" | yq '.storage.archive.s3[1].bucket_prefix')"

echo "=== 2: archive 1× S3 + 1× POSIX (mixed reader/writer) ==="
out=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.archive.s3[0].endpoint=http://s3' \
  --set 'global.archive.s3[0].accessKey=k' --set 'global.archive.s3[0].secretKey=s' \
  --set 'global.archive.s3[0].bucketPrefix=archive' \
  --set 'global.archive.posix[0].path=/legacy-archive' \
  --set 'global.archive.posix[0].writerDisabled=true' \
  --set 'global.archive.posix[0].volume.existingClaim=legacy-archive-pvc' \
  --show-only templates/finalize-secrets.yaml \
  2>/dev/null | yq '.stringData."config.yaml"')
check "2 has s3"                "1"    "$(echo "$out" | yq '.storage.archive.s3 | length')"
check "2 has posix"             "1"    "$(echo "$out" | yq '.storage.archive.posix | length')"
check "2 posix writer_disabled" "true" "$(echo "$out" | yq '.storage.archive.posix[0].writer_disabled')"

echo "=== 3: sync.destination 2× S3 ==="
out=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.archive.s3[0].endpoint=http://x' \
  --set 'global.archive.s3[0].accessKey=k' --set 'global.archive.s3[0].secretKey=s' \
  --set 'global.archive.s3[0].bucketPrefix=x' \
  --set 'global.sync.destination.s3[0].endpoint=http://d1' \
  --set 'global.sync.destination.s3[0].accessKey=k' --set 'global.sync.destination.s3[0].secretKey=s' \
  --set 'global.sync.destination.s3[0].bucketPrefix=d1' \
  --set 'global.sync.destination.s3[1].endpoint=http://d2' \
  --set 'global.sync.destination.s3[1].accessKey=k' --set 'global.sync.destination.s3[1].secretKey=s' \
  --set 'global.sync.destination.s3[1].bucketPrefix=d2' \
  --set global.schemaType=isolated \
  --set global.sync.remote.host=remote.example.com \
  --set global.sync.remote.user=u --set global.sync.remote.password=p \
  --set global.sync.api.user=u --set global.sync.api.pass=p \
  --set global.c4gh.syncPubKey=pk \
  --show-only templates/sync-secrets.yaml \
  2>/dev/null | yq '.stringData."config.yaml"')
check "3 sync entry count" "2" "$(echo "$out" | yq '.storage.sync.s3 | length')"

echo "=== 4: backupArchive 2× POSIX with mixed PVC + NFS ==="
out=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.archive.s3[0].endpoint=http://x' \
  --set 'global.archive.s3[0].accessKey=k' --set 'global.archive.s3[0].secretKey=s' \
  --set 'global.archive.s3[0].bucketPrefix=x' \
  --set 'global.backupArchive.posix[0].path=/bk_1' \
  --set 'global.backupArchive.posix[0].volume.existingClaim=bk-pvc-0' \
  --set 'global.backupArchive.posix[1].path=/bk_2' \
  --set 'global.backupArchive.posix[1].volume.nfsServer=nfs.example.com' \
  --set 'global.backupArchive.posix[1].volume.nfsPath=/exports/bk2' \
  --show-only templates/finalize-deploy.yaml \
  2>/dev/null)
check "4 backup-archive mount count" "2" \
  "$(echo "$out" | yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name | test("^backup-archive-")) | .mountPath' | wc -l | tr -d ' ')"
check "4 backup-archive-0 claim" "bk-pvc-0" \
  "$(echo "$out" | yq '.spec.template.spec.volumes[] | select(.name=="backup-archive-0") | .persistentVolumeClaim.claimName')"
check "4 backup-archive-1 nfs server" "nfs.example.com" \
  "$(echo "$out" | yq '.spec.template.spec.volumes[] | select(.name=="backup-archive-1") | .nfs.server')"

echo "=== 5: TLS-off s3 backend renders disable_https:true and no ca_cert ==="
# Gemini's coverage gap: verify TLS toggle actually drops ca_cert and flips
# disable_https. (TLS-on side requires many tls.secretName settings to render
# the whole chart; the off case is the high-value assertion.)
tls_off=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.archive.s3[0].endpoint=http://s3' \
  --set 'global.archive.s3[0].accessKey=k' --set 'global.archive.s3[0].secretKey=s' \
  --set 'global.archive.s3[0].bucketPrefix=x' \
  --show-only templates/finalize-secrets.yaml 2>/dev/null | yq '.stringData."config.yaml"')
check "5 tls-off s3[0].ca_cert absent"  "false" "$(echo "$tls_off" | yq '.storage.archive.s3[0] | has("ca_cert")')"
check "5 tls-off s3[0].disable_https"   "true"  "$(echo "$tls_off" | yq '.storage.archive.s3[0].disable_https')"

echo "=== 6: DOA preserves un-suffixed 'archive' volume name (static template check) ==="
# DOA reads only index 0 of archive.posix. Its k8s volume/mount must stay
# named 'archive' (no '-0' suffix) so the legacy Java app does not break.
# Helm-render needs a deep TLS-cascade to instantiate DOA, so static grep
# is the cheap reliable assertion here.
doa_arc=$(grep -cE "^[[:space:]]+- name: archive$" charts/sda-svc/templates/doa-deploy.yaml || true)
if [ "$doa_arc" = "2" ]; then
  echo "  PASS  6 doa-deploy.yaml has 'name: archive' twice (volumeMount + volume)"
  pass=$((pass+1))
else
  echo "  FAIL  6 expected 2 'name: archive' lines in doa-deploy.yaml, got $doa_arc"
  fail=$((fail+1))
fi
# Guard against accidental switch to suffixed name (archive-0).
doa_suffixed=$(grep -cE "name: archive-0" charts/sda-svc/templates/doa-deploy.yaml || true)
if [ "$doa_suffixed" = "0" ]; then
  echo "  PASS  6 doa-deploy.yaml has no 'archive-0' name"
  pass=$((pass+1))
else
  echo "  FAIL  6 doa-deploy.yaml uses suffixed 'archive-0' name ($doa_suffixed occurrence)"
  fail=$((fail+1))
fi

echo
echo "Summary: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
