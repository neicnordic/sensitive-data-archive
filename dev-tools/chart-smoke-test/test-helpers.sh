#!/usr/bin/env bash
# Storage helper render assertions. Each Phase B/C/D task appends a
# scenario here.
set -euo pipefail
ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

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
  # Defaults so required-backend secrets render without each test having
  # to set both archive and inbox. Tests override the index-0 fields.
  --set 'global.archive.s3[0].endpoint=http://default-archive'
  --set 'global.archive.s3[0].accessKey=k' --set 'global.archive.s3[0].secretKey=s'
  --set 'global.archive.s3[0].bucketPrefix=default-archive'
  --set 'global.inbox.s3[0].endpoint=http://default-inbox'
  --set 'global.inbox.s3[0].accessKey=k' --set 'global.inbox.s3[0].secretKey=s'
  --set 'global.inbox.s3[0].bucketPrefix=default-inbox'
  # Defaults for the s3-inbox SERVICE config (global.s3Inbox), used when
  # tests trigger external rendering that pulls s3-inbox-secrets in.
  --set 'global.s3Inbox.url=http://default-s3inbox'
  --set 'global.s3Inbox.accessKey=k' --set 'global.s3Inbox.secretKey=s'
  --set 'global.s3Inbox.bucket=default-s3inbox'
)

pass=0; fail=0
check() {
    local name="$1" expected="$2" actual="$3"
    if [ "$actual" = "$expected" ]; then
        echo "  PASS  $name"; pass=$((pass+1))
    else
        echo "  FAIL  $name (expected '$expected', got '$actual')"; fail=$((fail+1))
    fi
}

echo "=== A2: storageBackend helper + locationBrokerCacheTTL ==="

# Two-endpoint archive renders under .storage.archive.s3, snake_case keys
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.archive.s3[0].endpoint=https://s3a.example.com' \
  --set 'global.archive.s3[0].accessKey=keya' \
  --set 'global.archive.s3[0].secretKey=seca' \
  --set 'global.archive.s3[0].bucketPrefix=archive-a' \
  --set 'global.archive.s3[1].endpoint=https://s3b.example.com' \
  --set 'global.archive.s3[1].accessKey=keyb' \
  --set 'global.archive.s3[1].secretKey=secb' \
  --set 'global.archive.s3[1].bucketPrefix=archive-b' \
  --set 'global.archive.locationBrokerCacheTTL=120s' \
  --show-only templates/finalize-secrets.yaml \
  2>/dev/null | yq '.stringData."config.yaml"')

check "A2 archive s3 length" "2" "$(echo "$out" | yq '.storage.archive.s3 | length')"
check "A2 archive s3[1] bucket_prefix" "archive-b" "$(echo "$out" | yq '.storage.archive.s3[1].bucket_prefix')"
check "A2 location_broker at root" "120s" "$(echo "$out" | yq '.location_broker.cache_ttl')"
check "A2 location_broker NOT under storage" "null" "$(echo "$out" | yq '.storage.archive.location_broker // "null"')"

echo "=== A3: posixVolumeMounts + posixVolumes ==="

# Two-entry archive POSIX with mixed PVC + NFS backings
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.archive.s3=null' \
  --set 'global.archive.posix[0].path=/archive_1' \
  --set 'global.archive.posix[0].volume.existingClaim=arc-pvc-0' \
  --set 'global.archive.posix[1].path=/archive_2' \
  --set 'global.archive.posix[1].volume.nfsServer=nfs.example.com' \
  --set 'global.archive.posix[1].volume.nfsPath=/exports/arc2' \
  --show-only templates/ingest-deploy.yaml \
  2>/dev/null)

check "A3 archive volumeMounts count" "2" \
  "$(echo "$out" | yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name | test("^archive-")) | .mountPath' | wc -l | tr -d ' ')"
check "A3 archive-0 claim" "arc-pvc-0" \
  "$(echo "$out" | yq '.spec.template.spec.volumes[] | select(.name=="archive-0") | .persistentVolumeClaim.claimName')"
check "A3 archive-1 nfs server" "nfs.example.com" \
  "$(echo "$out" | yq '.spec.template.spec.volumes[] | select(.name=="archive-1") | .nfs.server')"

# Lowercase-only names — backupArchive becomes backup-archive (DNS-1123)
out_bk=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.archive.s3[0].endpoint=http://x' \
  --set 'global.archive.s3[0].accessKey=k' --set 'global.archive.s3[0].secretKey=s' \
  --set 'global.archive.s3[0].bucketPrefix=x' \
  --set 'global.backupArchive.posix[0].path=/bk_1' \
  --set 'global.backupArchive.posix[0].volume.existingClaim=bk-0' \
  --show-only templates/finalize-deploy.yaml \
  2>/dev/null)
check "A3 backup-archive-0 volume name (lowercase)" "bk-0" \
  "$(echo "$out_bk" | yq '.spec.template.spec.volumes[] | select(.name=="backup-archive-0") | .persistentVolumeClaim.claimName')"

echo "=== B1: finalize-secrets backupArchive two-endpoint ==="
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.archive.s3[0].endpoint=http://a' \
  --set 'global.archive.s3[0].accessKey=k' --set 'global.archive.s3[0].secretKey=s' \
  --set 'global.archive.s3[0].bucketPrefix=a' \
  --set 'global.backupArchive.s3[0].endpoint=http://b1' \
  --set 'global.backupArchive.s3[0].accessKey=k' --set 'global.backupArchive.s3[0].secretKey=s' \
  --set 'global.backupArchive.s3[0].bucketPrefix=b1' \
  --set 'global.backupArchive.s3[1].endpoint=http://b2' \
  --set 'global.backupArchive.s3[1].accessKey=k' --set 'global.backupArchive.s3[1].secretKey=s' \
  --set 'global.backupArchive.s3[1].bucketPrefix=b2' \
  --show-only templates/finalize-secrets.yaml \
  2>/dev/null | yq '.stringData."config.yaml"')
check "B1 backup s3 length" "2" "$(echo "$out" | yq '.storage.backup.s3 | length')"
check "B1 backup s3[1] bucket_prefix" "b2" "$(echo "$out" | yq '.storage.backup.s3[1].bucket_prefix')"

echo "=== B2: verify-secrets archive ==="
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.archive.s3[0].endpoint=http://va' --set 'global.archive.s3[0].accessKey=k' \
  --set 'global.archive.s3[0].secretKey=s' --set 'global.archive.s3[0].bucketPrefix=va' \
  --set 'global.archive.s3[1].endpoint=http://vb' --set 'global.archive.s3[1].accessKey=k' \
  --set 'global.archive.s3[1].secretKey=s' --set 'global.archive.s3[1].bucketPrefix=vb' \
  --show-only templates/verify-secrets.yaml \
  2>/dev/null | yq '.stringData."config.yaml"')
check "B2 verify archive s3 length" "2" "$(echo "$out" | yq '.storage.archive.s3 | length')"

echo "=== B3: ingest-secrets archive + inbox ==="
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.archive.s3[0].endpoint=http://ia' --set 'global.archive.s3[0].accessKey=k' \
  --set 'global.archive.s3[0].secretKey=s' --set 'global.archive.s3[0].bucketPrefix=ia' \
  --set 'global.archive.s3[1].endpoint=http://ib' --set 'global.archive.s3[1].accessKey=k' \
  --set 'global.archive.s3[1].secretKey=s' --set 'global.archive.s3[1].bucketPrefix=ib' \
  --set 'global.inbox.s3[0].endpoint=http://inbox' --set 'global.inbox.s3[0].accessKey=k' \
  --set 'global.inbox.s3[0].secretKey=s' --set 'global.inbox.s3[0].bucketPrefix=inbox' \
  --show-only templates/ingest-secrets.yaml \
  2>/dev/null | yq '.stringData."config.yaml"')
check "B3 ingest archive s3 length" "2" "$(echo "$out" | yq '.storage.archive.s3 | length')"
check "B3 ingest inbox s3 length"   "1" "$(echo "$out" | yq '.storage.inbox.s3 | length')"

echo "=== B4: sync-secrets archive + destination ==="
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set global.schemaType=isolated \
  --set global.sync.remote.host=r --set global.sync.remote.user=u --set global.sync.remote.password=p \
  --set global.c4gh.syncPubKey=pk \
  --set 'global.archive.s3[0].endpoint=http://sa' --set 'global.archive.s3[0].accessKey=k' \
  --set 'global.archive.s3[0].secretKey=s' --set 'global.archive.s3[0].bucketPrefix=sa' \
  --set 'global.sync.destination.s3[0].endpoint=http://d1' --set 'global.sync.destination.s3[0].accessKey=k' \
  --set 'global.sync.destination.s3[0].secretKey=s' --set 'global.sync.destination.s3[0].bucketPrefix=d1' \
  --set 'global.sync.destination.s3[1].endpoint=http://d2' --set 'global.sync.destination.s3[1].accessKey=k' \
  --set 'global.sync.destination.s3[1].secretKey=s' --set 'global.sync.destination.s3[1].bucketPrefix=d2' \
  --show-only templates/sync-secrets.yaml \
  2>/dev/null | yq '.stringData."config.yaml"')
check "B4 sync archive s3 length" "1" "$(echo "$out" | yq '.storage.archive.s3 | length')"
check "B4 sync.destination s3 length" "2" "$(echo "$out" | yq '.storage.sync.s3 | length')"

echo "=== B5: api-secrets inbox ==="
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set global.deploymentType=external \
  --set global.reencrypt.host=re \
  --set global.api.jwtPubKeyName=jwt.pub \
  --set global.ingress.hostName.api=api.example.com \
  --set global.ingress.hostName.download=dl.example.com \
  --set global.ingress.hostName.s3Inbox=s3.example.com \
  --set global.s3Inbox.url=http://si --set global.s3Inbox.accessKey=k \
  --set global.s3Inbox.secretKey=s --set global.s3Inbox.bucket=in \
  --set global.oidc.id=id --set global.oidc.secret=s --set global.oidc.provider=http://o \
  --set 'global.inbox.s3[0].endpoint=http://ai1' --set 'global.inbox.s3[0].accessKey=k' \
  --set 'global.inbox.s3[0].secretKey=s' --set 'global.inbox.s3[0].bucketPrefix=ai1' \
  --set 'global.inbox.s3[1].endpoint=http://ai2' --set 'global.inbox.s3[1].accessKey=k' \
  --set 'global.inbox.s3[1].secretKey=s' --set 'global.inbox.s3[1].bucketPrefix=ai2' \
  --set global.api.jwtSecret=j --set global.api.rbacFileSecret=r \
  --show-only templates/api-secrets.yaml \
  2>/dev/null | yq 'select(.metadata.name == "test-sda-svc-api") | .stringData."config.yaml"')
check "B5 api inbox s3 length" "2" "$(echo "$out" | yq '.storage.inbox.s3 | length')"

echo "=== B6: mapper-secrets inbox ==="
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.inbox.s3[0].endpoint=http://m1' --set 'global.inbox.s3[0].accessKey=k' \
  --set 'global.inbox.s3[0].secretKey=s' --set 'global.inbox.s3[0].bucketPrefix=m1' \
  --set 'global.inbox.s3[1].endpoint=http://m2' --set 'global.inbox.s3[1].accessKey=k' \
  --set 'global.inbox.s3[1].secretKey=s' --set 'global.inbox.s3[1].bucketPrefix=m2' \
  --show-only templates/mapper-secrets.yaml \
  2>/dev/null | yq '.stringData."config.yaml"')
check "B6 mapper inbox s3 length" "2" "$(echo "$out" | yq '.storage.inbox.s3 | length')"

echo "=== B7: download-secrets archive ==="
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set global.deploymentType=external \
  --set global.download.enabled=true \
  --set global.ingress.hostName.download=dl.t \
  --set global.ingress.hostName.api=api.t \
  --set global.ingress.hostName.auth=auth.t \
  --set global.ingress.hostName.s3Inbox=s3.t \
  --set global.reencrypt.host=r \
  --set global.api.jwtPubKeyName=jwt \
  --set global.api.jwtSecret=js \
  --set global.api.rbacFileSecret=rfs \
  --set 'global.archive.s3[0].endpoint=http://d1' --set 'global.archive.s3[0].accessKey=k' \
  --set 'global.archive.s3[0].secretKey=s' --set 'global.archive.s3[0].bucketPrefix=d1' \
  --set 'global.archive.s3[1].endpoint=http://d2' --set 'global.archive.s3[1].accessKey=k' \
  --set 'global.archive.s3[1].secretKey=s' --set 'global.archive.s3[1].bucketPrefix=d2' \
  --show-only templates/download-secrets.yaml \
  2>/dev/null | yq 'select(.metadata.name == "test-sda-svc-download") | .stringData."config.yaml"')
check "B7 download archive s3 length" "2" "$(echo "$out" | yq '.storage.archive.s3 | length')"
check "B7 download archive s3[1] endpoint" "http://d2" "$(echo "$out" | yq '.storage.archive.s3[1].endpoint')"

echo "=== B8: download-v2-secrets archive ==="
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set global.deploymentType=external \
  --set global.downloadV2.enabled=true \
  --set global.downloadV2.service.orgName=T \
  --set global.downloadV2.service.orgURL=http://t \
  --set global.oidc.id=id --set global.oidc.secret=s --set global.oidc.provider=http://o \
  --set global.ingress.hostName.downloadV2=v2.t \
  --set global.ingress.hostName.api=api.t \
  --set global.ingress.hostName.auth=auth.t \
  --set global.ingress.hostName.s3Inbox=s3.t \
  --set global.ingress.hostName.download=dl.t \
  --set global.api.jwtPubKeyName=jwt --set global.api.jwtSecret=js \
  --set global.api.rbacFileSecret=rfs \
  --set global.reencrypt.host=r \
  --set downloadV2.replicaCount=1 \
  --set 'global.archive.s3[0].endpoint=http://v2a' --set 'global.archive.s3[0].accessKey=k' \
  --set 'global.archive.s3[0].secretKey=s' --set 'global.archive.s3[0].bucketPrefix=v2a' \
  --set 'global.archive.s3[1].endpoint=http://v2b' --set 'global.archive.s3[1].accessKey=k' \
  --set 'global.archive.s3[1].secretKey=s' --set 'global.archive.s3[1].bucketPrefix=v2b' \
  --show-only templates/download-v2-secrets.yaml \
  2>/dev/null | yq 'select(.metadata.name == "test-sda-svc-download-v2") | .stringData."config.yaml"')
check "B8 download-v2 archive s3 length" "2" "$(echo "$out" | yq '.storage.archive.s3 | length')"
check "B8 download-v2 archive s3[1] bucket_prefix" "v2b" "$(echo "$out" | yq '.storage.archive.s3[1].bucket_prefix')"

echo "=== B9: shared-secrets reads list index 0 ==="
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.archive.s3[0].endpoint=http://a' --set 'global.archive.s3[0].accessKey=NEWKEY' \
  --set 'global.archive.s3[0].secretKey=NEWSEC' --set 'global.archive.s3[0].bucketPrefix=a' \
  --set 'global.backupArchive.s3[0].endpoint=http://b' --set 'global.backupArchive.s3[0].accessKey=BK' \
  --set 'global.backupArchive.s3[0].secretKey=BKS' --set 'global.backupArchive.s3[0].bucketPrefix=b' \
  --set 'global.inbox.s3[0].endpoint=http://i' --set 'global.inbox.s3[0].accessKey=IN' \
  --set 'global.inbox.s3[0].secretKey=INS' --set 'global.inbox.s3[0].bucketPrefix=i' \
  --show-only templates/shared-secrets.yaml 2>/dev/null)

check "B9 archive access key (base64)" "$(printf 'NEWKEY' | base64)" \
  "$(echo "$out" | yq 'select(.metadata.name == "test-sda-svc-s3archive-keys") | .data.s3ArchiveAccessKey')"
check "B9 backup access key (base64)"  "$(printf 'BK' | base64)" \
  "$(echo "$out" | yq 'select(.metadata.name == "test-sda-svc-s3backup-keys") | .data.s3BackupAccessKey')"
check "B9 inbox access key (base64)"   "$(printf 'IN' | base64)" \
  "$(echo "$out" | yq 'select(.metadata.name == "test-sda-svc-s3inbox-keys") | .data.s3InboxAccessKey')"

echo "=== C1: ingest-deploy inbox posix ==="
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.archive.s3[0].endpoint=http://a' --set 'global.archive.s3[0].accessKey=k' \
  --set 'global.archive.s3[0].secretKey=s' --set 'global.archive.s3[0].bucketPrefix=a' \
  --set 'global.inbox.s3=null' \
  --set 'global.inbox.posix[0].path=/inbox_1' \
  --set 'global.inbox.posix[0].volume.existingClaim=inb-pvc-0' \
  --set 'global.inbox.posix[1].path=/inbox_2' \
  --set 'global.inbox.posix[1].volume.nfsServer=nfs.example.com' \
  --set 'global.inbox.posix[1].volume.nfsPath=/exports/inb2' \
  --show-only templates/ingest-deploy.yaml 2>/dev/null)
check "C1 ingest inbox mount count" "2" \
  "$(echo "$out" | yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name | test("^inbox-")) | .mountPath' | wc -l | tr -d ' ')"
check "C1 ingest inbox-0 claim" "inb-pvc-0" \
  "$(echo "$out" | yq '.spec.template.spec.volumes[] | select(.name=="inbox-0") | .persistentVolumeClaim.claimName')"
check "C1 ingest inbox-1 nfs" "nfs.example.com" \
  "$(echo "$out" | yq '.spec.template.spec.volumes[] | select(.name=="inbox-1") | .nfs.server')"

echo "=== C2: verify-deploy archive posix ==="
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.archive.s3=null' \
  --set 'global.archive.posix[0].path=/v_arc_1' \
  --set 'global.archive.posix[0].volume.existingClaim=v-pvc-0' \
  --set 'global.archive.posix[1].path=/v_arc_2' \
  --set 'global.archive.posix[1].volume.existingClaim=v-pvc-1' \
  --show-only templates/verify-deploy.yaml 2>/dev/null)
check "C2 verify archive mount count" "2" \
  "$(echo "$out" | yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name | test("^archive-")) | .mountPath' | wc -l | tr -d ' ')"
check "C2 verify archive-1 claim" "v-pvc-1" \
  "$(echo "$out" | yq '.spec.template.spec.volumes[] | select(.name=="archive-1") | .persistentVolumeClaim.claimName')"

echo "=== C3: sync-deploy archive + sync.destination posix ==="
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set global.schemaType=isolated \
  --set global.sync.remote.host=r --set global.sync.remote.user=u --set global.sync.remote.password=p \
  --set global.c4gh.syncPubKey=pk \
  --set global.sync.api.user=apiuser --set global.sync.api.pass=apipass \
  --set 'global.archive.s3=null' \
  --set 'global.archive.posix[0].path=/s_arc_1' \
  --set 'global.archive.posix[0].volume.existingClaim=s-pvc-0' \
  --set 'global.archive.posix[1].path=/s_arc_2' \
  --set 'global.archive.posix[1].volume.existingClaim=s-pvc-1' \
  --set 'global.sync.destination.posix[0].path=/sync_dest' \
  --set 'global.sync.destination.posix[0].volume.existingClaim=dest-pvc' \
  --show-only templates/sync-deploy.yaml 2>/dev/null)
check "C3 sync archive mount count" "2" \
  "$(echo "$out" | yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name | test("^archive-")) | .mountPath' | wc -l | tr -d ' ')"
check "C3 sync-dest mount count" "1" \
  "$(echo "$out" | yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name | test("^sync-dest-")) | .mountPath' | wc -l | tr -d ' ')"
check "C3 sync-dest-0 claim" "dest-pvc" \
  "$(echo "$out" | yq '.spec.template.spec.volumes[] | select(.name=="sync-dest-0") | .persistentVolumeClaim.claimName')"

echo "=== C4: finalize-deploy archive posix ==="
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.archive.s3=null' \
  --set 'global.archive.posix[0].path=/f_arc_1' \
  --set 'global.archive.posix[0].volume.existingClaim=f-arc-0' \
  --set 'global.backupArchive.posix[0].path=/f_bk_1' \
  --set 'global.backupArchive.posix[0].volume.existingClaim=f-bk-0' \
  --show-only templates/finalize-deploy.yaml 2>/dev/null)
check "C4 finalize archive mount count" "1" \
  "$(echo "$out" | yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name | test("^archive-")) | .mountPath' | wc -l | tr -d ' ')"
check "C4 finalize archive-0 claim" "f-arc-0" \
  "$(echo "$out" | yq '.spec.template.spec.volumes[] | select(.name=="archive-0") | .persistentVolumeClaim.claimName')"

echo "=== C5: api-deploy inbox posix ==="
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set global.deploymentType=external \
  --set 'global.inbox.s3=null' \
  --set 'global.inbox.posix[0].path=/api_in_1' \
  --set 'global.inbox.posix[0].volume.existingClaim=api-in-0' \
  --set 'global.inbox.posix[1].path=/api_in_2' \
  --set 'global.inbox.posix[1].volume.existingClaim=api-in-1' \
  --set global.api.jwtSecret=j --set global.api.rbacFileSecret=r \
  --set 'global.archive.s3[0].endpoint=http://a' --set 'global.archive.s3[0].accessKey=k' \
  --set 'global.archive.s3[0].secretKey=s' --set 'global.archive.s3[0].bucketPrefix=a' \
  --set global.reencrypt.host=r \
  --set global.api.jwtPubKeyName=jwt \
  --set global.ingress.hostName.api=api.t \
  --set global.ingress.hostName.auth=auth.t \
  --set global.ingress.hostName.s3Inbox=s3.t \
  --set global.ingress.hostName.download=dl.t \
  --show-only templates/api-deploy.yaml 2>/dev/null)
check "C5 api inbox mount count" "2" \
  "$(echo "$out" | yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name | test("^inbox-")) | .mountPath' | wc -l | tr -d ' ')"
check "C5 api inbox-1 claim" "api-in-1" \
  "$(echo "$out" | yq '.spec.template.spec.volumes[] | select(.name=="inbox-1") | .persistentVolumeClaim.claimName')"

echo "=== C6: mapper-deploy inbox posix ==="
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.inbox.s3=null' \
  --set 'global.inbox.posix[0].path=/m_in_1' \
  --set 'global.inbox.posix[0].volume.existingClaim=m-in-0' \
  --show-only templates/mapper-deploy.yaml 2>/dev/null)
check "C6 mapper inbox mount count" "1" \
  "$(echo "$out" | yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name | test("^inbox-")) | .mountPath' | wc -l | tr -d ' ')"

echo "=== C7: download-deploy archive posix ==="
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set global.deploymentType=external \
  --set global.download.enabled=true \
  --set global.ingress.hostName.download=dl.t \
  --set global.ingress.hostName.api=api.t \
  --set global.ingress.hostName.auth=auth.t \
  --set global.ingress.hostName.s3Inbox=s3.t \
  --set global.api.jwtSecret=js --set global.api.rbacFileSecret=rfs \
  --set global.api.jwtPubKeyName=jwt \
  --set global.reencrypt.host=r \
  --set 'global.archive.s3=null' \
  --set 'global.archive.posix[0].path=/dl_arc_1' \
  --set 'global.archive.posix[0].volume.existingClaim=dl-arc-0' \
  --show-only templates/download-deploy.yaml 2>/dev/null)
check "C7 download archive mount count" "1" \
  "$(echo "$out" | yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name | test("^archive-")) | .mountPath' | wc -l | tr -d ' ')"
check "C7 download archive-0 claim" "dl-arc-0" \
  "$(echo "$out" | yq '.spec.template.spec.volumes[] | select(.name=="archive-0") | .persistentVolumeClaim.claimName')"

echo "=== C8: download-v2-deploy archive posix ==="
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set global.deploymentType=external \
  --set global.downloadV2.enabled=true \
  --set global.downloadV2.service.orgName=T \
  --set global.downloadV2.service.orgURL=http://t \
  --set global.oidc.id=id --set global.oidc.secret=s --set global.oidc.provider=http://o \
  --set global.ingress.hostName.downloadV2=v2.t \
  --set global.ingress.hostName.api=api.t \
  --set global.ingress.hostName.auth=auth.t \
  --set global.ingress.hostName.s3Inbox=s3.t \
  --set global.ingress.hostName.download=dl.t \
  --set global.api.jwtPubKeyName=jwt --set global.api.jwtSecret=js \
  --set global.api.rbacFileSecret=rfs \
  --set global.reencrypt.host=r \
  --set downloadV2.replicaCount=1 \
  --set 'global.archive.s3=null' \
  --set 'global.archive.posix[0].path=/v2_arc_1' \
  --set 'global.archive.posix[0].volume.existingClaim=v2-arc-0' \
  --set 'global.archive.posix[1].path=/v2_arc_2' \
  --set 'global.archive.posix[1].volume.existingClaim=v2-arc-1' \
  --show-only templates/download-v2-deploy.yaml 2>/dev/null)
check "C8 v2 download archive mount count" "2" \
  "$(echo "$out" | yq '.spec.template.spec.containers[0].volumeMounts[] | select(.name | test("^archive-")) | .mountPath' | wc -l | tr -d ' ')"
check "C8 v2 download archive-1 claim" "v2-arc-1" \
  "$(echo "$out" | yq '.spec.template.spec.volumes[] | select(.name=="archive-1") | .persistentVolumeClaim.claimName')"

echo "=== C9: sftp-inbox-deploy posix list shape ==="
out=$(helm template test charts/sda-svc \
  --set global.deploymentType=external \
  --set global.tls.enabled=true --set global.tls.issuer=ti \
  --set global.db.host=db --set global.db.user=u --set global.db.password=p \
  --set global.broker.host=mq --set global.broker.username=u --set global.broker.password=p \
  --set global.c4gh.secretName=c --set global.c4gh.publicKey=p \
  --set 'global.c4gh.privateKeys[0].keyName=k' \
  --set 'global.c4gh.privateKeys[0].passphrase=p' \
  --set 'global.c4gh.privateKeys[0].keyData=d' \
  --set 'global.inbox.s3=null' \
  --set 'global.inbox.posix[0].path=/sftp_in' \
  --set 'global.inbox.posix[0].volume.existingClaim=sftp-pvc' \
  --set 'global.archive.s3[0].endpoint=http://a' --set 'global.archive.s3[0].accessKey=k' \
  --set 'global.archive.s3[0].secretKey=s' --set 'global.archive.s3[0].bucketPrefix=a' \
  --set global.cega.user=u --set global.cega.password=p \
  --set global.ingress.hostName.api=api.t \
  --set global.ingress.hostName.auth=auth.t \
  --set global.ingress.hostName.s3Inbox=s3.t \
  --set global.ingress.hostName.download=dl.t \
  --set global.api.jwtSecret=j --set global.api.rbacFileSecret=r \
  --set global.api.jwtPubKeyName=jwt \
  --set global.reencrypt.host=r \
  --set sftpInbox.tls.issuer=ti \
  --show-only templates/sftp-inbox-deploy.yaml 2>/dev/null)
check "C9 sftp inbox-0 claim" "sftp-pvc" \
  "$(echo "$out" | yq '.spec.template.spec.volumes[] | select(.name=="inbox-0") | .persistentVolumeClaim.claimName')"

echo "=== D1: DOA env names + fail-fast ==="
# Single endpoint reads index 0 with correct env names
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set global.tls.enabled=true --set global.tls.issuer=ti \
  --set global.deploymentType=external \
  --set global.doa.enabled=true \
  --set 'global.archive.s3[0].endpoint=http://doa1' \
  --set 'global.archive.s3[0].accessKey=k' --set 'global.archive.s3[0].secretKey=s' \
  --set 'global.archive.s3[0].bucketPrefix=doa1' \
  --set 'global.archive.s3[0].port=9000' \
  --set global.ingress.hostName.api=api.t \
  --set global.ingress.hostName.auth=auth.t \
  --set global.ingress.hostName.s3Inbox=s3.t \
  --set global.ingress.hostName.download=dl.t \
  --set global.ingress.hostName.doa=doa.t \
  --set global.api.jwtSecret=j --set global.api.rbacFileSecret=r \
  --set global.api.jwtPubKeyName=jwt \
  --set global.reencrypt.host=r \
  --set global.oidc.id=id --set global.oidc.secret=s --set global.oidc.provider=http://o \
  --show-only templates/doa-deploy.yaml 2>/dev/null || true)
check "D1 DOA S3_ENDPOINT (scheme stripped for Java MinIO)" "doa1" \
  "$(echo "$out" | yq '.spec.template.spec.containers[] | select(.name=="doa") | .env[] | select(.name=="S3_ENDPOINT") | .value')"
check "D1 DOA S3_PORT" "9000" \
  "$(echo "$out" | yq '.spec.template.spec.containers[] | select(.name=="doa") | .env[] | select(.name=="S3_PORT") | .value')"
check "D1 DOA S3_SECURE follows scheme (http -> false)" "false" \
  "$(echo "$out" | yq '.spec.template.spec.containers[] | select(.name=="doa") | .env[] | select(.name=="S3_SECURE") | .value')"
check "D1 DOA S3_BUCKET" "doa1" \
  "$(echo "$out" | yq '.spec.template.spec.containers[] | select(.name=="doa") | .env[] | select(.name=="S3_BUCKET") | .value')"
check "D1 DOA S3_ACCESS_KEY uses s3archive-keys secret" "test-sda-svc-s3archive-keys" \
  "$(echo "$out" | yq '.spec.template.spec.containers[] | select(.name=="doa") | .env[] | select(.name=="S3_ACCESS_KEY") | .valueFrom.secretKeyRef.name')"

# Multi-endpoint + DOA enabled → fail-fast (1 s3 + 1 posix should fire)
if helm template test charts/sda-svc "${MINIMAL[@]}" \
    --set global.tls.enabled=true --set global.tls.issuer=ti \
    --set global.deploymentType=external \
    --set global.doa.enabled=true \
    --set 'global.archive.s3[0].endpoint=http://doa1' --set 'global.archive.s3[0].accessKey=k' \
    --set 'global.archive.s3[0].secretKey=s' --set 'global.archive.s3[0].bucketPrefix=doa1' \
    --set 'global.archive.posix[0].path=/p' --set 'global.archive.posix[0].volume.existingClaim=p' \
    --set global.ingress.hostName.api=api.t --set global.ingress.hostName.auth=auth.t \
    --set global.ingress.hostName.s3Inbox=s3.t --set global.ingress.hostName.download=dl.t \
    --set global.ingress.hostName.doa=doa.t \
    --set global.api.jwtSecret=j --set global.api.rbacFileSecret=r \
    --set global.api.jwtPubKeyName=jwt --set global.reencrypt.host=r \
    --set global.oidc.id=id --set global.oidc.secret=s --set global.oidc.provider=http://o \
    --show-only templates/doa-deploy.yaml >/dev/null 2>&1; then
  echo "  FAIL  D1 fail-fast didn't fire on 1 s3 + 1 posix"; fail=$((fail+1))
else
  echo "  PASS  D1 fail-fast fires on multi-endpoint + DOA"; pass=$((pass+1))
fi

echo "=== D2: inbox-secrets guards accept list shape ==="
out=$(helm template test charts/sda-svc "${MINIMAL[@]}" \
  --set global.deploymentType=external \
  --set global.s3Inbox.url=http://si --set global.s3Inbox.accessKey=k \
  --set global.s3Inbox.secretKey=s --set global.s3Inbox.bucket=in \
  --set 'global.inbox.s3[0].endpoint=http://x' --set 'global.inbox.s3[0].accessKey=k' \
  --set 'global.inbox.s3[0].secretKey=s' --set 'global.inbox.s3[0].bucketPrefix=x' \
  --set 'global.archive.s3[0].endpoint=http://a' --set 'global.archive.s3[0].accessKey=k' \
  --set 'global.archive.s3[0].secretKey=s' --set 'global.archive.s3[0].bucketPrefix=a' \
  --set global.ingress.hostName.api=api.t --set global.ingress.hostName.auth=auth.t \
  --set global.ingress.hostName.s3Inbox=s3.t --set global.ingress.hostName.download=dl.t \
  --set global.api.jwtSecret=j --set global.api.rbacFileSecret=r \
  --set global.api.jwtPubKeyName=jwt --set global.reencrypt.host=r \
  --set global.oidc.id=id --set global.oidc.secret=s --set global.oidc.provider=http://o \
  --show-only templates/s3-inbox-secrets.yaml 2>/dev/null | yq '.metadata.name')
if echo "$out" | grep -q inbox; then
  echo "  PASS  D2 s3-inbox-secrets renders with inbox.s3 list"; pass=$((pass+1))
else
  echo "  FAIL  D2 s3-inbox-secrets not rendered (out: $out)"; fail=$((fail+1))
fi

echo "=== D3: auth/ingress/service/release-test guards accept list shape ==="
for tmpl in auth-deploy auth-secrets auth-ingress auth-service auth-certificate s3-inbox-ingress s3-inbox-deploy inbox-service release-test-deploy; do
  extra=()
  case "$tmpl" in
    release-test-deploy)
      ext=yml
      extra=(--set releasetest.run=true --set releasetest.secrets.accessToken=t)
      ;;
    auth-certificate)
      ext=yaml
      extra=(--set global.tls.enabled=true --set global.tls.clusterIssuer=ca)
      ;;
    *) ext=yaml ;;
  esac
  name=$(helm template test charts/sda-svc \
    --set global.deploymentType=external \
    --set global.tls.enabled=false \
    --set global.db.host=db --set global.db.user=u --set global.db.password=p \
    --set global.broker.host=mq --set global.broker.username=u --set global.broker.password=p \
    --set global.c4gh.secretName=c --set global.c4gh.publicKey=p \
    --set 'global.c4gh.privateKeys[0].keyName=k' \
    --set 'global.c4gh.privateKeys[0].passphrase=p' \
    --set 'global.c4gh.privateKeys[0].keyData=d' \
    --set global.ingress.deploy=true \
    --set global.ingress.hostName.api=api.t \
    --set global.ingress.hostName.auth=auth.t \
    --set global.ingress.hostName.s3Inbox=inbox.t \
    --set global.ingress.hostName.download=dl.t \
    --set global.s3Inbox.url=http://si --set global.s3Inbox.accessKey=k \
    --set global.s3Inbox.secretKey=s --set global.s3Inbox.bucket=in \
    --set 'global.inbox.s3[0].endpoint=http://x' --set 'global.inbox.s3[0].accessKey=k' \
    --set 'global.inbox.s3[0].secretKey=s' --set 'global.inbox.s3[0].bucketPrefix=x' \
    --set 'global.archive.s3[0].endpoint=http://a' --set 'global.archive.s3[0].accessKey=k' \
    --set 'global.archive.s3[0].secretKey=s' --set 'global.archive.s3[0].bucketPrefix=a' \
    --set global.api.jwtSecret=j --set global.api.rbacFileSecret=r \
    --set global.api.jwtPubKeyName=jwt --set global.reencrypt.host=r \
    --set global.oidc.id=id --set global.oidc.secret=s --set global.oidc.provider=http://o \
    --set global.cega.host=h --set global.cega.user=u --set global.cega.password=p \
    ${extra[@]+"${extra[@]}"} \
    --show-only "templates/$tmpl.$ext" 2>/dev/null | yq 'select(.metadata.name != null) | .metadata.name' 2>/dev/null || true)
  name=$(printf '%s\n' "$name" | awk 'NF{print; exit}')
  if [ -n "$name" ] && [ "$name" != "null" ]; then
    echo "  PASS  D3 $tmpl renders ($name)"; pass=$((pass+1))
  else
    echo "  FAIL  D3 $tmpl did not render"; fail=$((fail+1))
  fi
done

echo "=== E1: legacy scalar keys absent from values.yaml ==="
fail_keys=""
for backend in archive backupArchive inbox; do
  for key in storageType s3Url s3Port s3BucketPrefix s3Region s3AccessKey s3SecretKey s3ChunkSize s3CaFile volumePath nfsServer nfsPath existingClaim maxBuckets maxObjects maxSize; do
    val=$(yq ".global.$backend.$key // \"__absent__\"" charts/sda-svc/values.yaml)
    [ "$val" = "__absent__" ] || fail_keys+="global.$backend.$key still in values.yaml; "
  done
done
for key in s3Url s3Port s3BucketPrefix s3Region s3AccessKey s3SecretKey s3ChunkSize s3CaFile maxBuckets maxObjects maxSize; do
  val=$(yq ".global.sync.destination.$key // \"__absent__\"" charts/sda-svc/values.yaml)
  [ "$val" = "__absent__" ] || fail_keys+="global.sync.destination.$key still in values.yaml; "
done
if [ -z "$fail_keys" ]; then
  echo "  PASS  no legacy keys in values.yaml"; pass=$((pass+1))
else
  echo "  FAIL  $fail_keys"; fail=$((fail+1))
fi

echo "=== E1: legacy scalar refs absent from templates ==="
legacy_in_tmpls=$(grep -rn -E "\.Values\.global\.(archive|backupArchive|inbox|sync\.destination)\.(storageType|s3Url|s3Port|s3BucketPrefix|s3Region|s3AccessKey|s3SecretKey|s3ChunkSize|s3CaFile|volumePath|nfsServer|nfsPath|existingClaim|maxBuckets|maxObjects|maxSize)" charts/sda-svc/templates/ || true)
if [ -z "$legacy_in_tmpls" ]; then
  echo "  PASS  no legacy refs in templates"; pass=$((pass+1))
else
  echo "  FAIL  legacy refs still in templates:"; echo "$legacy_in_tmpls"; fail=$((fail+1))
fi

echo "=== G4: sftp-inbox INBOX_LOCATION tracks inbox.posix[0].path ==="
# Pre-fix the chart mounted inbox.posix[0].path but the Java app still
# wrote to its hardcoded default /ega/inbox/. Verify the env var is set.
g4_args=(
  "${MINIMAL[@]}"
  --set global.tls.enabled=true --set global.tls.issuer=ti
  --set global.deploymentType=external
  --set global.ingress.hostName.api=api.t --set global.ingress.hostName.auth=auth.t
  --set global.ingress.hostName.s3Inbox=s3.t --set global.ingress.hostName.download=dl.t
  --set global.api.jwtSecret=j --set global.api.rbacFileSecret=r
  --set global.api.jwtPubKeyName=jwt --set global.reencrypt.host=r
  --set global.oidc.id=id --set global.oidc.secret=s --set global.oidc.provider=http://o
  --set 'global.archive.s3[0].endpoint=http://a'
  --set 'global.archive.s3[0].accessKey=k' --set 'global.archive.s3[0].secretKey=s'
  --set 'global.archive.s3[0].bucketPrefix=a'
)
g4_out=$(helm template t charts/sda-svc "${g4_args[@]}" \
  --set 'global.inbox.s3=null' \
  --set 'global.inbox.posix[0].path=/sftp_inbox' \
  --set 'global.inbox.posix[0].volume.existingClaim=inb-pvc' \
  --show-only templates/sftp-inbox-deploy.yaml 2>/dev/null || true)
inbox_loc=$(echo "$g4_out" | yq '.spec.template.spec.containers[] | select(.name=="inbox") | .env[] | select(.name=="INBOX_LOCATION") | .value')
if [ "$inbox_loc" = "/sftp_inbox" ]; then
  echo "  PASS  G4 INBOX_LOCATION=/sftp_inbox matches posix[0].path"; pass=$((pass+1))
else
  echo "  FAIL  G4 expected INBOX_LOCATION=/sftp_inbox, got '$inbox_loc'"; fail=$((fail+1))
fi
mount_path=$(echo "$g4_out" | yq '.spec.template.spec.containers[] | select(.name=="inbox") | .volumeMounts[] | select(.name | test("^inbox-")) | .mountPath')
if [ "$mount_path" = "/sftp_inbox" ]; then
  echo "  PASS  G4 sftp-inbox container mounts at posix[0].path (alignment)"; pass=$((pass+1))
else
  echo "  FAIL  G4 expected mountPath=/sftp_inbox, got '$mount_path'"; fail=$((fail+1))
fi

echo "=== G3: DOA endpoint URL normalization ==="
# DOA's Java MinIO client takes host + port + secure separately, while
# storage-v2 takes a URL with scheme. Verify the chart splits a URL with
# embedded port and infers S3_SECURE from the scheme.
g3_args=(
  "${MINIMAL[@]}"
  --set global.tls.enabled=true --set global.tls.issuer=ti
  --set global.deploymentType=external --set global.doa.enabled=true
  --set global.ingress.hostName.api=api.t --set global.ingress.hostName.auth=auth.t
  --set global.ingress.hostName.s3Inbox=s3.t --set global.ingress.hostName.download=dl.t
  --set global.ingress.hostName.doa=doa.t
  --set global.api.jwtSecret=j --set global.api.rbacFileSecret=r
  --set global.api.jwtPubKeyName=jwt --set global.reencrypt.host=r
  --set global.oidc.id=id --set global.oidc.secret=s --set global.oidc.provider=http://o
)
# G3a: https URL with embedded port -> host stripped, port = URL port, secure=true
g3a=$(helm template t charts/sda-svc "${g3_args[@]}" \
  --set 'global.archive.s3[0].endpoint=https://minio:9000' \
  --set 'global.archive.s3[0].accessKey=k' --set 'global.archive.s3[0].secretKey=s' \
  --set 'global.archive.s3[0].bucketPrefix=a' \
  --show-only templates/doa-deploy.yaml 2>/dev/null || true)
check_doa() { local name="$1" expected="$2" env_name="$3" src="$4"
  local got
  got=$(echo "$src" | yq ".spec.template.spec.containers[] | select(.name==\"doa\") | .env[] | select(.name==\"${env_name}\") | .value")
  if [ "$got" = "$expected" ]; then echo "  PASS  $name (${env_name}=${expected})"; pass=$((pass+1))
  else echo "  FAIL  $name expected ${env_name}='${expected}' got '${got}'"; fail=$((fail+1)); fi
}
check_doa "G3a https+port: host stripped"       "minio" S3_ENDPOINT "$g3a"
check_doa "G3a https+port: port from URL"       "9000"  S3_PORT     "$g3a"
check_doa "G3a https+port: secure=true"         "true"  S3_SECURE   "$g3a"

# G3b: https URL without port -> default port 443
g3b=$(helm template t charts/sda-svc "${g3_args[@]}" \
  --set 'global.archive.s3[0].endpoint=https://minio' \
  --set 'global.archive.s3[0].accessKey=k' --set 'global.archive.s3[0].secretKey=s' \
  --set 'global.archive.s3[0].bucketPrefix=a' \
  --show-only templates/doa-deploy.yaml 2>/dev/null || true)
check_doa "G3b https no port: default port 443" "443"   S3_PORT     "$g3b"
check_doa "G3b https no port: secure=true"      "true"  S3_SECURE   "$g3b"

# G3c: http URL with port -> secure=false
g3c=$(helm template t charts/sda-svc "${g3_args[@]}" \
  --set 'global.archive.s3[0].endpoint=http://minio:9000' \
  --set 'global.archive.s3[0].accessKey=k' --set 'global.archive.s3[0].secretKey=s' \
  --set 'global.archive.s3[0].bucketPrefix=a' \
  --show-only templates/doa-deploy.yaml 2>/dev/null || true)
check_doa "G3c http+port: secure=false"         "false" S3_SECURE   "$g3c"
check_doa "G3c http+port: port from URL"        "9000"  S3_PORT     "$g3c"

echo "=== G5: empty-list storage fail-fast (per backend) ==="
# Pre-fix the chart rendered an empty storage: block; services would
# crash at startup. Each required-backend secrets template now runs
# sda.requireBackend first.
g5_args=(
  --set global.deploymentType=internal --set global.tls.enabled=false
  --set global.db.host=db --set global.db.user=u --set global.db.password=p
  --set global.broker.host=mq --set global.broker.username=u --set global.broker.password=p
  --set global.c4gh.secretName=c --set global.c4gh.publicKey=p
  --set 'global.c4gh.privateKeys[0].keyName=k'
  --set 'global.c4gh.privateKeys[0].passphrase=p'
  --set 'global.c4gh.privateKeys[0].keyData=d'
  --set global.cega.host=h --set global.cega.user=u --set global.cega.password=p
  --set global.ingress.deploy=false
)
# G5a: missing archive entirely -> verify-secrets / ingest-secrets fail
g5a=$(helm template t charts/sda-svc "${g5_args[@]}" 2>&1 || true)
if echo "$g5a" | grep -q "global.archive must define at least one s3 or posix endpoint"; then
  echo "  PASS  G5a empty archive triggers fail-fast"; pass=$((pass+1))
else
  echo "  FAIL  G5a expected archive fail-fast, got first 5 lines:"
  head -5 <<<"$g5a" | sed 's/^/    /'
  fail=$((fail+1))
fi
# G5b: archive set but inbox missing -> ingest/mapper-secrets fail
g5b=$(helm template t charts/sda-svc "${g5_args[@]}" \
  --set 'global.archive.s3[0].endpoint=http://a' \
  --set 'global.archive.s3[0].accessKey=k' --set 'global.archive.s3[0].secretKey=s' \
  --set 'global.archive.s3[0].bucketPrefix=a' \
  2>&1 || true)
if echo "$g5b" | grep -q "global.inbox must define at least one s3 or posix endpoint"; then
  echo "  PASS  G5b empty inbox triggers fail-fast"; pass=$((pass+1))
else
  echo "  FAIL  G5b expected inbox fail-fast, got first 5 lines:"
  head -5 <<<"$g5b" | sed 's/^/    /'
  fail=$((fail+1))
fi

echo "=== G2: doa.enabled=true + tls.enabled=false → fail-fast ==="
# Pre-fix the chart silently rendered no DOA deployment when TLS was off,
# letting operators believe DOA was deployed. Chart-level helper catches it.
g2_out=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
  --set global.doa.enabled=true \
  --set 'global.archive.s3[0].endpoint=https://x' \
  --set 'global.archive.s3[0].accessKey=k' --set 'global.archive.s3[0].secretKey=s' \
  --set 'global.archive.s3[0].bucketPrefix=a' \
  --set 'global.backupArchive.s3[0].endpoint=https://x' \
  --set 'global.backupArchive.s3[0].accessKey=k' --set 'global.backupArchive.s3[0].secretKey=s' \
  --set 'global.backupArchive.s3[0].bucketPrefix=b' \
  --set 'global.inbox.s3[0].endpoint=https://x' \
  --set 'global.inbox.s3[0].accessKey=k' --set 'global.inbox.s3[0].secretKey=s' \
  --set 'global.inbox.s3[0].bucketPrefix=i' \
  2>&1 || true)
if echo "$g2_out" | grep -q "doa.enabled=true requires global.tls.enabled=true"; then
  echo "  PASS  doa.enabled=true + tls=false triggers fail-fast"; pass=$((pass+1))
else
  echo "  FAIL  doa.enabled=true + tls=false did not trigger fail-fast"; fail=$((fail+1))
fi

echo "=== G1: storage lists tolerate --set <list>=null (no len-of-nil panic) ==="
# Reproduces the bug Gemini caught: --set 'global.X.s3=null' must not
# crash chart rendering with `len of nil pointer`. Same path the CI POSIX
# matrix takes in .github/integration/scripts/charts/deploy_charts.sh.
null_out=$(helm template charts/sda-svc \
    --set 'global.archive.s3=null' \
    --set 'global.archive.posix[0].path=/archive' \
    --set 'global.archive.posix[0].volume.existingClaim=archive-pvc' \
    --set 'global.backupArchive.s3=null' \
    --set 'global.backupArchive.posix[0].path=/backup' \
    --set 'global.backupArchive.posix[0].volume.existingClaim=backup-pvc' \
    --set 'global.inbox.s3=null' \
    --set 'global.inbox.posix[0].path=/inbox' \
    --set 'global.inbox.posix[0].volume.existingClaim=inbox-pvc' \
    --show-only templates/ingest-secrets.yaml 2>&1 || true)
if echo "$null_out" | grep -q "len of nil pointer"; then
  echo "  FAIL  --set <list>=null still panics on len-of-nil"
  echo "    ${null_out//$'\n'/$'\n    '}"
  fail=$((fail+1))
else
  echo "  PASS  --set <list>=null does not panic (POSIX matrix CI path)"
  pass=$((pass+1))
fi

echo "=== G6: legacy 3.x scalar key fail-fast (non-empty value) ==="
# detectLegacyStorageKeys helper should fail with a migration message
# whenever any legacy scalar (s3Url, storageType, volumePath, ...) is
# set to a non-empty value on archive/backupArchive/inbox/sync.destination.
# Empty/default values must not trip the guard.
for key in s3Url s3BucketPrefix storageType volumePath maxBuckets; do
  g6_out=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
    --set "global.archive.${key}=legacy-value" \
    --show-only templates/shared-secrets.yaml 2>&1 || true)
  if echo "$g6_out" | grep -q "Legacy storage key global.archive.${key} detected"; then
    echo "  PASS  legacy archive.${key} triggers fail-fast"; pass=$((pass+1))
  else
    echo "  FAIL  legacy archive.${key} did not trigger fail-fast"
    echo "    ${g6_out//$'\n'/$'\n    '}"
    fail=$((fail+1))
  fi
done
# sync.destination path goes through the helper's separate branch.
g6_sync=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
  --set global.schemaType=isolated \
  --set global.sync.remote.host=remote.example.com \
  --set global.sync.remote.user=u --set global.sync.remote.password=p \
  --set 'global.sync.destination.s3Url=https://legacy' \
  --show-only templates/shared-secrets.yaml 2>&1 || true)
if echo "$g6_sync" | grep -q "Legacy storage key global.sync.destination.s3Url detected"; then
  echo "  PASS  legacy sync.destination.s3Url triggers fail-fast"; pass=$((pass+1))
else
  echo "  FAIL  legacy sync.destination.s3Url did not trigger fail-fast"
  fail=$((fail+1))
fi

echo "=== G7: mixed inbox.s3 + inbox.posix → fail-fast ==="
# The chart deploys two distinct inbox services (s3-inbox and sftp-inbox)
# under one Service selector. Populating both lists is unsupported.
g7_out=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.inbox.posix[0].path=/inbox' \
  --set 'global.inbox.posix[0].volume.existingClaim=inbox-pvc' \
  --show-only templates/shared-secrets.yaml 2>&1 || true)
if echo "$g7_out" | grep -q "global.inbox cannot have both s3 and posix"; then
  echo "  PASS  mixed inbox.s3 + inbox.posix triggers fail-fast"; pass=$((pass+1))
else
  echo "  FAIL  mixed inbox.s3 + inbox.posix did not trigger fail-fast"
  echo "    ${g7_out//$'\n'/$'\n    '}"
  fail=$((fail+1))
fi

echo "=== G8: location_broker.cache_ttl priority across backends ==="
# Sync uses [syncDestination, archive], finalize uses [backupArchive, archive].
# Higher-priority backend's TTL wins when both are set.
g8_sync=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
  --set global.schemaType=isolated \
  --set global.sync.remote.host=remote.example.com \
  --set global.sync.remote.user=u --set global.sync.remote.password=p \
  --set global.sync.api.user=u --set global.sync.api.pass=p \
  --set global.c4gh.syncPubKey=pk \
  --set 'global.archive.locationBrokerCacheTTL=11s' \
  --set 'global.sync.destination.locationBrokerCacheTTL=99s' \
  --set 'global.sync.destination.s3[0].endpoint=http://d' \
  --set 'global.sync.destination.s3[0].accessKey=k' \
  --set 'global.sync.destination.s3[0].secretKey=s' \
  --set 'global.sync.destination.s3[0].bucketPrefix=d' \
  --show-only templates/sync-secrets.yaml 2>/dev/null \
  | yq '.stringData."config.yaml"' | yq '.location_broker.cache_ttl')
check "sync prefers destination over archive TTL" "99s" "$g8_sync"

# Setting only archive on sync — archive value should bubble up.
g8_sync_arc=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
  --set global.schemaType=isolated \
  --set global.sync.remote.host=remote.example.com \
  --set global.sync.remote.user=u --set global.sync.remote.password=p \
  --set global.sync.api.user=u --set global.sync.api.pass=p \
  --set global.c4gh.syncPubKey=pk \
  --set 'global.archive.locationBrokerCacheTTL=42s' \
  --set 'global.sync.destination.s3[0].endpoint=http://d' \
  --set 'global.sync.destination.s3[0].accessKey=k' \
  --set 'global.sync.destination.s3[0].secretKey=s' \
  --set 'global.sync.destination.s3[0].bucketPrefix=d' \
  --show-only templates/sync-secrets.yaml 2>/dev/null \
  | yq '.stringData."config.yaml"' | yq '.location_broker.cache_ttl')
check "sync falls back to archive TTL" "42s" "$g8_sync_arc"

# Finalize prefers backupArchive over archive.
g8_fin=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.archive.locationBrokerCacheTTL=11s' \
  --set 'global.backupArchive.locationBrokerCacheTTL=77s' \
  --set 'global.backupArchive.s3[0].endpoint=http://b' \
  --set 'global.backupArchive.s3[0].accessKey=k' \
  --set 'global.backupArchive.s3[0].secretKey=s' \
  --set 'global.backupArchive.s3[0].bucketPrefix=b' \
  --show-only templates/finalize-secrets.yaml 2>/dev/null \
  | yq '.stringData."config.yaml"' | yq '.location_broker.cache_ttl')
check "finalize prefers backupArchive over archive TTL" "77s" "$g8_fin"

echo "=== G9: helper required-errors name the backend + list index ==="
# Errors emitted by sda.storageBackend / sda.posixVolumes should be
# self-locating (operator sees "global.archive.s3[1].secretKey is
# required" instead of just "S3 secret_key required").
g9_s3=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.archive.s3[1].endpoint=http://second' \
  --set 'global.archive.s3[1].accessKey=k' \
  --set 'global.archive.s3[1].bucketPrefix=second' \
  --show-only templates/finalize-secrets.yaml 2>&1 || true)
if echo "$g9_s3" | grep -q "global.archive.s3\[1\].secretKey is required"; then
  echo "  PASS  S3 secret-key error names backend + index"; pass=$((pass+1))
else
  echo "  FAIL  S3 secret-key error missing backend + index hint"
  echo "    ${g9_s3//$'\n'/$'\n    '}"
  fail=$((fail+1))
fi

g9_pos=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
  --set global.inbox.s3=null \
  --set 'global.inbox.posix[0].path=/inbox' \
  --show-only templates/sftp-inbox-deploy.yaml 2>&1 || true)
if echo "$g9_pos" | grep -q "global.inbox.posix\[0\].volume is required"; then
  echo "  PASS  POSIX missing-volume error names backend + index"; pass=$((pass+1))
else
  echo "  FAIL  POSIX missing-volume error missing backend + index hint"
  echo "    ${g9_pos//$'\n'/$'\n    '}"
  fail=$((fail+1))
fi

# backupArchive and sync.destination — the helper's emitted yaml key
# ("backup", "sync") differs from the values path operators must set.
# Errors must name the values path, not the yaml key.
g9_backup=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.backupArchive.s3[0].endpoint=http://b' \
  --set 'global.backupArchive.s3[0].accessKey=k' \
  --set 'global.backupArchive.s3[0].bucketPrefix=b' \
  --show-only templates/finalize-secrets.yaml 2>&1 || true)
if echo "$g9_backup" | grep -q "global.backupArchive.s3\[0\].secretKey is required"; then
  echo "  PASS  backupArchive error uses values path (not yaml key 'backup')"; pass=$((pass+1))
else
  echo "  FAIL  backupArchive error did not use values path"
  echo "    ${g9_backup//$'\n'/$'\n    '}"
  fail=$((fail+1))
fi

g9_sync=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
  --set global.schemaType=isolated \
  --set global.sync.remote.host=remote.example.com \
  --set global.sync.remote.user=u --set global.sync.remote.password=p \
  --set global.sync.api.user=u --set global.sync.api.pass=p \
  --set global.c4gh.syncPubKey=pk \
  --set 'global.sync.destination.s3[0].endpoint=http://d' \
  --set 'global.sync.destination.s3[0].accessKey=k' \
  --set 'global.sync.destination.s3[0].bucketPrefix=d' \
  --show-only templates/sync-secrets.yaml 2>&1 || true)
if echo "$g9_sync" | grep -q "global.sync.destination.s3\[0\].secretKey is required"; then
  echo "  PASS  sync.destination error uses values path (not yaml key 'sync')"; pass=$((pass+1))
else
  echo "  FAIL  sync.destination error did not use values path"
  echo "    ${g9_sync//$'\n'/$'\n    '}"
  fail=$((fail+1))
fi

echo "=== G10: multi-writer s3 + posix → fail-fast (writer backends only) ==="
# storage-v2 returns ErrorMultipleWritersNotSupported when archive,
# backupArchive, or sync.destination has writer-enabled entries in both
# s3 and posix. Chart fails fast at template time. Readers (entries
# with writerDisabled: true) are exempt.
for backend_pair in \
    'global.archive,archive' \
    'global.backupArchive,backupArchive'; do
  prefix="${backend_pair%,*}"
  name="${backend_pair##*,}"
  out=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
    --set "${prefix}.s3[0].endpoint=http://s3" \
    --set "${prefix}.s3[0].accessKey=k" --set "${prefix}.s3[0].secretKey=s" \
    --set "${prefix}.s3[0].bucketPrefix=${name}" \
    --set "${prefix}.posix[0].path=/${name}" \
    --set "${prefix}.posix[0].volume.existingClaim=pvc" \
    2>&1 || true)
  if echo "$out" | grep -q "global.${name} has writer-enabled entries in both s3 and posix"; then
    echo "  PASS  ${name}: mixed writer-enabled s3+posix triggers fail-fast"; pass=$((pass+1))
  else
    echo "  FAIL  ${name}: mixed writer-enabled s3+posix not rejected"
    echo "    ${out//$'\n'/$'\n    '}"
    fail=$((fail+1))
  fi
done
# sync.destination needs schemaType=isolated + remote info to even render.
sync_mixed=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
  --set global.schemaType=isolated \
  --set global.sync.remote.host=remote.example.com \
  --set global.sync.remote.user=u --set global.sync.remote.password=p \
  --set global.sync.api.user=u --set global.sync.api.pass=p \
  --set global.c4gh.syncPubKey=pk \
  --set 'global.sync.destination.s3[0].endpoint=http://s' \
  --set 'global.sync.destination.s3[0].accessKey=k' \
  --set 'global.sync.destination.s3[0].secretKey=s' \
  --set 'global.sync.destination.s3[0].bucketPrefix=s' \
  --set 'global.sync.destination.posix[0].path=/sync' \
  --set 'global.sync.destination.posix[0].volume.existingClaim=pvc' \
  2>&1 || true)
if echo "$sync_mixed" | grep -q "global.sync.destination has writer-enabled entries in both s3 and posix"; then
  echo "  PASS  sync.destination: mixed writer-enabled s3+posix triggers fail-fast"; pass=$((pass+1))
else
  echo "  FAIL  sync.destination: mixed writer-enabled s3+posix not rejected"
  echo "    ${sync_mixed//$'\n'/$'\n    '}"
  fail=$((fail+1))
fi
# writerDisabled: true on either side → must NOT fail (one writer + one reader).
reader_ok=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.archive.s3[0].endpoint=http://w' \
  --set 'global.archive.s3[0].accessKey=k' --set 'global.archive.s3[0].secretKey=s' \
  --set 'global.archive.s3[0].bucketPrefix=w' \
  --set 'global.archive.posix[0].path=/reader' \
  --set 'global.archive.posix[0].volume.existingClaim=pvc' \
  --set 'global.archive.posix[0].writerDisabled=true' \
  --show-only templates/finalize-secrets.yaml 2>&1 || true)
if echo "$reader_ok" | grep -q "writer-enabled entries in both"; then
  echo "  FAIL  posix reader (writerDisabled=true) wrongly rejected as multi-writer"
  echo "    ${reader_ok//$'\n'/$'\n    '}"
  fail=$((fail+1))
else
  echo "  PASS  posix writerDisabled=true + s3 writer renders cleanly"; pass=$((pass+1))
fi
# Inverse: s3 reader + posix writer must also render.
inverse_ok=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
  --set 'global.archive.s3[0].endpoint=http://r' \
  --set 'global.archive.s3[0].accessKey=k' --set 'global.archive.s3[0].secretKey=s' \
  --set 'global.archive.s3[0].bucketPrefix=r' \
  --set 'global.archive.s3[0].writerDisabled=true' \
  --set 'global.archive.posix[0].path=/writer' \
  --set 'global.archive.posix[0].volume.existingClaim=pvc' \
  --show-only templates/finalize-secrets.yaml 2>&1 || true)
if echo "$inverse_ok" | grep -q "writer-enabled entries in both"; then
  echo "  FAIL  s3 reader (writerDisabled=true) + posix writer wrongly rejected"
  echo "    ${inverse_ok//$'\n'/$'\n    '}"
  fail=$((fail+1))
else
  echo "  PASS  s3 writerDisabled=true + posix writer renders cleanly"; pass=$((pass+1))
fi

echo "=== G11: sda.posixVolumes error uses values path for sync ==="
# Maps the internal "syncDestination" k8s alias to the values path
# "sync.destination" in self-locating errors.
g11=$(helm template t charts/sda-svc "${MINIMAL[@]}" \
  --set global.schemaType=isolated \
  --set global.sync.remote.host=remote.example.com \
  --set global.sync.remote.user=u --set global.sync.remote.password=p \
  --set global.sync.api.user=u --set global.sync.api.pass=p \
  --set global.c4gh.syncPubKey=pk \
  --set 'global.sync.destination.posix[0].path=/sync' \
  --show-only templates/sync-deploy.yaml 2>&1 || true)
if echo "$g11" | grep -q "global.sync.destination.posix\[0\].volume is required"; then
  echo "  PASS  posixVolumes error names global.sync.destination (not syncDestination)"; pass=$((pass+1))
else
  echo "  FAIL  posixVolumes error did not use values path for sync"
  echo "    ${g11//$'\n'/$'\n    '}"
  fail=$((fail+1))
fi

echo
echo "Summary: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
