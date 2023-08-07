#!/bin/bash
set -ex

if [ "$1" == "sda-db" ]; then
    ROOTPASS=$(yq e '.global.db.password' .github/integration/scripts/charts/values.yaml)
    helm install postgres charts/sda-db \
        --set image.tag=test-postgres \
        --set image.pullPolicy=Never \
        --set global.postgresAdminPassword="$ROOTPASS" \
        --set global.tls.enabled=false \
        --set persistence.enabled=false \
        --set resources=null \
        --wait
fi

if [ "$1" == "sda-mq" ]; then
    ADMINPASS=$(yq e '.global.broker.password' .github/integration/scripts/charts/values.yaml)
    helm install broker charts/sda-mq \
        --set image.tag=test-rabbitmq \
        --set image.pullPolicy=Never \
        --set global.adminPassword="$ADMINPASS" \
        --set global.adminUser=admin \
        --set global.tls.enabled=false \
        --set persistence.enabled=false \
        --set resources=null \
        --wait
fi

if [ "$1" == "sda-svc" ]; then
    helm install pipeline charts/sda-svc \
        --set image.tag=test \
        --set image.pullPolicy=Never \
        -f .github/integration/scripts/charts/values.yaml \
        --wait
fi
