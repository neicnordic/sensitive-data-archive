#!/bin/bash
set -e

if [ "$1" = "issuer" ]; then
    kubectl apply -f dev_tools/cega/cega-issuer.yaml
else
    kubectl create secret generic cega-certs \
        --from-file="sda-deploy-init/config"/certs/ca.crt \
        --from-file=tls.crt="sda-deploy-init/config"/certs/cega.crt \
        --from-file=tls.key="sda-deploy-init/config"/certs/cega.key
fi

kubectl create secret generic cega-users-config \
    --from-file=dev_tools/cega/users.json --from-file=dev_tools/cega/users.py

kubectl create secret generic cega-mq-config \
    --from-file=dev_tools/cega/cega.json --from-file=dev_tools/cega/cega.conf --from-file=dev_tools/cega/cega.plugins

kubectl apply -f dev_tools/cega/deploy.yaml
