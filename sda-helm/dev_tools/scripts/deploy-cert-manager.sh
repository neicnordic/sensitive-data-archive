#!/bin/bash
set -e

helm repo add jetstack https://charts.jetstack.io
helm repo update

helm install \
  cert-manager jetstack/cert-manager \
  --version v1.7.1 \
  --set installCRDs=true

kubectl apply -f dev_tools/config/cert-issuer.yaml
