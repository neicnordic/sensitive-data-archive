#!/bin/bash
set -ex

YQ_VERSION="v4.20.1"
C4GH_VERSION="$(curl --retry 100 -sL https://api.github.com/repos/neicnordic/crypt4gh/releases/latest | jq -r '.name')"

random-string() {
        head -c 32 /dev/urandom | base64 -w0 | tr -d '/+' | fold -w 32 | head -n 1
}

if [ "$1" == "local" ]; then
        if [ ! "$(command crypt4gh)" ]; then
                echo "crypt4gh not installed, get it from here: https://github.com/neicnordic/crypt4gh/releases/latest"
                exit 1
        elif [ "$(crypt4gh --version | cut -d ' ' -f1)" == "GA4GH" ]; then
                echo "This script requires the GO version of crypt4gh."
                echo "Get it from here: https://github.com/neicnordic/crypt4gh/releases/latest"
                exit 1
        fi

        if [ ! "$(command yq)" ]; then
                echo "yq not installed, get it from here: https://github.com/mikefarah/yq/releases/latest"
                exit 1
        fi
else
        sudo curl --retry 100 -sL "https://github.com/mikefarah/yq/releases/download/${YQ_VERSION}/yq_linux_amd64" -o /usr/bin/yq &&
                sudo chmod +x /usr/bin/yq

        curl --retry 100 -sL https://github.com/neicnordic/crypt4gh/releases/download/"${C4GH_VERSION}"/crypt4gh_linux_x86_64.tar.gz | sudo tar -xz -C /usr/bin/ &&
                sudo chmod +x /usr/bin/crypt4gh
fi

# secret for the crypt4gh keypair
C4GHPASSPHRASE="$(random-string)"
export C4GHPASSPHRASE
dir=$PWD
if [ -n "$1" ]; then
        dir=/tmp
fi
if [ ! -f "$dir/c4gh.sec.pem" ]; then
        crypt4gh generate -n "$dir/c4gh" -p "$C4GHPASSPHRASE"
fi
kubectl create secret generic c4gh --from-file="$dir/c4gh.sec.pem" --from-file="$dir/c4gh.pub.pem" --from-literal=passphrase="${C4GHPASSPHRASE}"
# secret for the OIDC keypair
openssl ecparam -name prime256v1 -genkey -noout -out "$dir/jwt.key"
openssl ec -in "$dir/jwt.key" -pubout -out "$dir/jwt.pub"
kubectl create secret generic jwk --from-file="$dir/jwt.key" --from-file="$dir/jwt.pub"

## OIDC
SELF=$(dirname "$0")
kubectl create configmap oidc --from-file="$SELF/../../sda/oidc.py"

helm repo add jetstack https://charts.jetstack.io
helm repo add minio https://charts.min.io/

helm repo update

helm install \
        cert-manager jetstack/cert-manager \
        --namespace cert-manager \
        --create-namespace \
        --set installCRDs=true

kubectl create namespace minio
kubectl apply -f .github/integration/scripts/charts/dependencies.yaml

## S3 storage backend
MINIO_ACCESS="$(random-string)"
export MINIO_ACCESS
MINIO_SECRET="$(random-string)"
export MINIO_SECRET
helm install minio minio/minio \
        --namespace minio \
        --set rootUser="$MINIO_ACCESS",rootPassword="$MINIO_SECRET",persistence.enabled=false,mode=standalone,resources.requests.memory=128Mi

PGPASSWORD="$(random-string)"
export PGPASSWORD

MQPASSWORD="$(random-string)"
export MQPASSWORD

TEST_TOKEN="$(bash .github/integration/scripts/sign_jwt.sh ES256 "$dir/jwt.key")"
export TEST_TOKEN

values_file=".github/integration/scripts/charts/values.yaml"
if [ "$1" == "local" ]; then
        values_file=/tmp/values.yaml
        cp .github/integration/scripts/charts/values.yaml /tmp/values.yaml
fi

## update values file with all credentials
yq -i '
.global.archive.s3AccessKey = strenv(MINIO_ACCESS) |
.global.archive.s3SecretKey = strenv(MINIO_SECRET) |
.global.backupArchive.s3AccessKey = strenv(MINIO_ACCESS) |
.global.backupArchive.s3SecretKey = strenv(MINIO_SECRET) |
.global.broker.password = strenv(MQPASSWORD) |
.global.c4gh.passphrase = strenv(C4GHPASSPHRASE) |
.global.db.password = strenv(PGPASSWORD) |
.global.inbox.s3AccessKey = strenv(MINIO_ACCESS) |
.global.inbox.s3SecretKey = strenv(MINIO_SECRET) |
.global.sync.destination.accessKey = strenv(MINIO_ACCESS) |
.global.sync.destination.secretKey = strenv(MINIO_SECRET) |
.releasetest.secrets.accessToken = strenv(TEST_TOKEN)
' "$values_file"

kubectl create secret generic api-rbac --from-file=".github/integration/sda/rbac.json"