#!/bin/bash
set -e

kubectl apply -f dev_tools/config/oidc.yaml

RETRY_TIMES=0
until kubectl get pods -l app=oidc-server -o jsonpath='{..status.containerStatuses[*].ready}' | grep "true"; do
    echo "waiting for oidc-server to become ready"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        kubectl describe pod -l app=oidc-server
        kubectl logs -l app=oidc-server
        exit 1
    fi
    sleep 10
done
