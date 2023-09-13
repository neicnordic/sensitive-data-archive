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

if [ "$1" == "sda-db" ]; then
    ROOTPASS=$(yq e '.global.db.password' .github/integration/scripts/charts/values.yaml)
    helm install postgres charts/sda-db \
        --set image.tag="PR$2-postgres" \
        --set image.pullPolicy=IfNotPresent \
        --set global.postgresAdminPassword="$ROOTPASS" \
        --set global.tls.clusterIssuer=cert-issuer \
        --set global.tls.enabled="$3" \
        --set persistence.enabled=false \
        --set resources=null \
        --wait
fi

if [ "$1" == "sda-mq" ]; then
    ADMINPASS=$(yq e '.global.broker.password' .github/integration/scripts/charts/values.yaml)
    helm install broker charts/sda-mq \
        --set image.tag="PR$2-rabbitmq" \
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
    inbox=s3
    if [ "$3" == "true" ] && [ "$4" == "posix" ]; then
        inbox=posix
    fi

    helm install pipeline charts/sda-svc \
        --set image.tag="PR$2" \
        --set image.pullPolicy=IfNotPresent \
        --set global.tls.enabled="$3" \
        --set global.broker.port="$MQ_PORT" \
        --set global.archive.storageType="$4" \
        --set global.inbox.storageType="$inbox" \
        -f .github/integration/scripts/charts/values.yaml \
        --wait
fi
