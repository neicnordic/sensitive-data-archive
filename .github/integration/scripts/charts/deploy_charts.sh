#!/bin/bash
set -ex

if [ -z "$2" ]; then
    echo "PR number missing"
    exit 1
fi

MQ_PORT=5672
if [ "$3" == "true" ]; then
    MQ_PORT=5671
fi

dir=".github/integration/scripts/charts"
if [ "$(yq e '.global.db.password' .github/integration/scripts/charts/values.yaml)" == "PLACEHOLDER_VALUE" ]; then
    dir=/tmp
fi

if [ "$1" == "sda-db" ]; then
    ROOTPASS="$(yq e '.global.db.password' "$dir/values.yaml")"
    helm install postgres charts/sda-db \
        --set image.tag="PR$2" \
        --set image.pullPolicy=IfNotPresent \
        --set global.postgresAdminPassword="$ROOTPASS" \
        --set global.tls.clusterIssuer=cert-issuer \
        --set global.tls.enabled="$3" \
        --set persistence.enabled=false \
        --set resources=null \
        --wait
fi

if [ "$1" == "sda-mq" ]; then
    ADMINPASS="$(yq e '.global.broker.password' "$dir/values.yaml")"
    helm install broker charts/sda-mq \
        --set image.tag="PR$2" \
        --set image.pullPolicy=IfNotPresent \
        --set global.adminPassword="$ADMINPASS" \
        --set global.adminUser=admin \
        --set global.tls.enabled="$3" \
        --set global.tls.clusterIssuer=cert-issuer \
        --set persistence.enabled=false \
        --set resources=null \
        --wait
fi

if [ "$1" == "sda-svc" ]; then
    sync_host=""
    if [ "$4" == "s3" ]; then
        sync_host=https://sync-api
        sync_api_pass=pass
        sync_api_user=user
    fi
    helm install pipeline charts/sda-svc \
        --set image.tag="PR$2" \
        --set image.pullPolicy=IfNotPresent \
        --set global.tls.enabled="$3" \
        --set global.broker.port="$MQ_PORT" \
        --set global.archive.storageType="$4" \
        --set global.backupArchive.storageType="$4" \
        --set global.inbox.storageType="$4" \
        --set global.sync.api.password="$sync_api_pass" \
        --set global.sync.api.user="$sync_api_user" \
        --set global.sync.remote.host="$sync_host" \
        -f "$dir/values.yaml" \
        --wait
fi
