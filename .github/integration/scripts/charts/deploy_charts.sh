#!/bin/bash
set -ex

if [ -z "$2" ];then
    echo "PR number missing"
    exit 1
fi

if [ "$1" == "sda-db" ]; then
    ROOTPASS=$(yq e '.global.db.password' .github/integration/scripts/charts/values.yaml)
    helm install postgres charts/sda-db \
        --set image.tag="PR$2-postgres" \
        --set image.pullPolicy=IfNotPresent \
        --set global.postgresAdminPassword="$ROOTPASS" \
        --set global.tls.enabled=false \
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
        --set global.tls.enabled=false \
        --set persistence.enabled=false \
        --set resources=null \
        --wait
fi

if [ "$1" == "sda-svc" ]; then
    helm install pipeline charts/sda-svc \
        --set image.tag="PR$2" \
        --set image.pullPolicy=IfNotPresent \
        -f .github/integration/scripts/charts/values.yaml \
        --wait
fi
