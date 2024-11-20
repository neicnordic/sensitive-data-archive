#!/bin/bash
set -ex

YQ_VERSION="v4.20.1"
C4GH_VERSION="$(curl --retry 100 -sL https://api.github.com/repos/neicnordic/crypt4gh/releases/latest | jq -r '.name')"

random-string() {
        head -c 32 /dev/urandom | base64 -w0 | tr -d '/+' | fold -w 32 | head -n 1
}

sudo curl --retry 100 -sLO "https://github.com/mikefarah/yq/releases/download/${YQ_VERSION}/yq_linux_amd64" -O /usr/bin/yq &&
        sudo chmod +x /usr/bin/yq

curl --retry 100 -sL https://github.com/neicnordic/crypt4gh/releases/download/"${C4GH_VERSION}"/crypt4gh_linux_x86_64.tar.gz | sudo tar -xz -C /usr/bin/ &&
        sudo chmod +x /usr/bin/crypt4gh

# secret for the crypt4gh keypair
C4GHPASSPHRASE="$(random-string)"
export C4GHPASSPHRASE
crypt4gh generate -n c4gh -p "$C4GHPASSPHRASE"
kubectl create secret generic c4gh --from-file="c4gh.sec.pem" --from-file="c4gh.pub.pem" --from-literal=passphrase="${C4GHPASSPHRASE}"
# secret for the OIDC keypair
openssl ecparam -name prime256v1 -genkey -noout -out "jwt.key"
openssl ec -in "jwt.key" -pubout -out "jwt.pub"
kubectl create secret generic jwk --from-file="jwt.key" --from-file="jwt.pub"

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

TEST_TOKEN="$(bash .github/integration/scripts/sign_jwt.sh ES256 jwt.key)"
export TEST_TOKEN

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
' .github/integration/scripts/charts/values.yaml

cat >rbac.json <<EOD
{
   "policy": [
      {
         "role": "admin",
         "path": "/c4gh-keys/*",
         "action": "(GET)|(POST)|(PUT)"
      },
      {
         "role": "submission",
         "path": "/file/ingest",
         "action": "POST"
      },
            {
         "role": "submission",
         "path": "/file/accession",
         "action": "POST"
      },
      {
         "role": "submission",
         "path": "/dataset/create",
         "action": "POST"
      },
      {
         "role": "submission",
         "path": "/dataset/release/*dataset",
         "action": "POST"
      },
      {
         "role": "submission",
         "path": "/users",
         "action": "GET"
      },
      {
         "role": "submission",
         "path": "/users/:username/files",
         "action": "GET"
      },
      {
         "role": "*",
         "path": "/files",
         "action": "GET"
      }
   ],
   "roles": [
      {
         "role": "admin",
         "rolebinding": "submission"
      },
      {
         "role": "requester@demo.org",
         "rolebinding": "admin"
      },
      {
         "role": "dummy@example.com",
         "rolebinding": "admin"
      }
   ]
}
EOD

kubectl create secret generic api-rbac --from-file="rbac.json"