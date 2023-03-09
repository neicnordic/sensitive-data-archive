#!/bin/bash
set -e

helm repo add minio https://helm.min.io/
helm repo update

MINIO_ACCESS=$(grep s3_access_key sda-deploy-init/config/trace.yml | awk '{print $2}' | sed -e 's/\"//g')
MINIO_SECRET=$(grep s3_secret_key sda-deploy-init/config/trace.yml | awk '{print $2}' | sed -e 's/\"//g')

if [ "$1" = "issuer" ]; then
    kubectl apply -f dev_tools/config/minio-issuer.yaml

    helm install minio minio/minio \
        --set accessKey="$MINIO_ACCESS",secretKey="$MINIO_SECRET",tls.enabled=true,tls.certSecret=minio-certs,tls.publicCrt=tls.crt,tls.privateKey=tls.key,persistence.enabled=false,service.port=443 --version 8.0.8
else
    kubectl create secret generic minio-certs --from-file=sda-deploy-init/config/certs/public.crt --from-file=sda-deploy-init/config/certs/private.key

    helm install minio minio/minio \
        --set accessKey="$MINIO_ACCESS",secretKey="$MINIO_SECRET",tls.enabled=true,tls.certSecret=minio-certs,persistence.enabled=false,service.port=443 --version 8.0.8
fi
