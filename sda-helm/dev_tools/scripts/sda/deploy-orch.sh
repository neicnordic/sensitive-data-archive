#!/bin/bash
set -e

## sda-orch certs
kubectl create secret generic orch-certs \
--from-file=sda-deploy-init/config/certs/ca.crt \
--from-file=sda-deploy-init/config/certs/orch.crt \
--from-file=sda-deploy-init/config/certs/orch.key

helm install orch charts/sda-orch -f dev_tools/config/orch.yaml
