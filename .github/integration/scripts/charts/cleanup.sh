#!/bin/bash

helm -n cert-manager uninstall cert-manager
kubectl delete ns cert-manager || true

helm -n minio uninstall minio
kubectl delete ns minio || true

kubectl delete secrets c4gh jwk || true
kubectl delete cm oidc || true
kubectl delete deployment.apps/oidc-server || true
kubectl delete service/oidc-server || true

helm uninstall pipeline || true
helm uninstall broker || true
helm uninstall postgres || true

kubectl delete secrets api-rbac broker-sda-mq-certs broker-sda-mq-test-certs postgres-sda-db-certs postgres-sda-db-test-certs || true
