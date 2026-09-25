#!/bin/bash
set -ex

YQ_VERSION="v4.20.1"
C4GH_VERSION="v1.14.0"

random-string() {
        head -c 32 /dev/urandom | base64 -w0 | tr -d '/+' | fold -w 32 | head -n 1
}

if [ "$1" == "local" ]; then
        if [ ! "$(command crypt4gh --version)" ]; then
                echo "crypt4gh not installed, get it from here: https://github.com/neicnordic/crypt4gh/releases/latest"
                exit 1
        elif [ "$(crypt4gh --version | cut -d ' ' -f1)" == "GA4GH" ]; then
                echo "This script requires the GO version of crypt4gh."
                echo "Get it from here: https://github.com/neicnordic/crypt4gh/releases/latest"
                exit 1
        fi

        if [ ! "$(command yq --version)" ]; then
                echo "yq not installed, get it from here: https://github.com/mikefarah/yq/releases/latest"
                exit 1
        fi

        if [ ! "$(command jq --version)" ]; then
                echo "jq not installed"
                exit 1
        fi

        if [ ! "$(command xxd --version 2>&1)" ]; then
                echo "xxd not installed"
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
helm repo add nfs-ganesha-server-and-external-provisioner https://kubernetes-sigs.github.io/nfs-ganesha-server-and-external-provisioner/

helm repo update

helm install \
        cert-manager jetstack/cert-manager \
        --namespace cert-manager \
        --create-namespace \
        --set installCRDs=true

helm install --namespace default nfs-ganesha nfs-ganesha-server-and-external-provisioner/nfs-server-provisioner --set "storageClass.mountOptions={tcp,nfsvers=4.1,retrans=2,timeo=30}"

kubectl create namespace ceph
kubectl apply -f .github/integration/scripts/charts/dependencies.yaml


values_file=".github/integration/scripts/charts/values.yaml"
if [ "$1" == "local" ]; then
  values_file=/tmp/values.yaml
  cp .github/integration/scripts/charts/values.yaml /tmp/values.yaml
fi

## Single-container Ceph RGW as S3 backend, exposed as service s3.ceph on port 9000.
## The startup script is shared with the compose based integration tests.
deploy_ceph_rgw() {
    kubectl -n ceph create configmap ceph-rgw \
        --from-file=.github/integration/scripts/ceph-rgw.sh \
        --from-file=.github/integration/scripts/s3.py
    kubectl -n ceph create secret generic ceph-rgw-credentials \
        --from-literal=S3_ACCESS_KEY="$S3_ACCESS" \
        --from-literal=S3_SECRET_KEY="$S3_SECRET"

    local tls_env="" tls_mount="" tls_volume=""
    if [ "$1" = true ]; then
        tls_env='{"name": "RGW_TLS_CERT", "value": "/certs/tls.crt"}, {"name": "RGW_TLS_KEY", "value": "/certs/tls.key"}'
        tls_mount='{"name": "certs", "mountPath": "/certs", "readOnly": true},'
        tls_volume='{"name": "certs", "secret": {"secretName": "s3-cert"}},'
    fi

    kubectl -n ceph apply -f - <<EOF
{
  "apiVersion": "apps/v1",
  "kind": "Deployment",
  "metadata": {"name": "s3"},
  "spec": {
    "replicas": 1,
    "selector": {"matchLabels": {"app": "s3"}},
    "template": {
      "metadata": {"labels": {"app": "s3"}},
      "spec": {
        "containers": [{
          "name": "rgw",
          "image": "quay.io/ceph/vstart-cluster:19.2.6",
          "command": ["/scripts/ceph-rgw.sh"],
          "env": [$tls_env],
          "envFrom": [{"secretRef": {"name": "ceph-rgw-credentials"}}],
          "ports": [{"containerPort": 9000}],
          "readinessProbe": {"exec": {"command": ["/scripts/ceph-rgw.sh", "health"]}, "periodSeconds": 2},
          "resources": {"requests": {"memory": "256Mi"}},
          "volumeMounts": [$tls_mount {"name": "scripts", "mountPath": "/scripts"}]
        }],
        "volumes": [$tls_volume {"name": "scripts", "configMap": {"name": "ceph-rgw", "defaultMode": 493}}]
      }
    }
  }
}
EOF
    kubectl -n ceph expose deployment s3 --port=9000 --target-port=9000
}

if [ "$2" == "s3" ]; then
  if [ "$3" = true ] ; then
    ## S3 storage backend
    S3_ACCESS="$(random-string)"
    export S3_ACCESS
    S3_SECRET="$(random-string)"
    export S3_SECRET
    deploy_ceph_rgw true

    yq -i '
.global.archive.s3[0].endpoint = "https://s3.ceph" |
.global.backupArchive.s3[0].endpoint = "https://s3.ceph" |
.global.inbox.s3[0].endpoint = "https://s3.ceph" |
.global.s3Inbox.url = "https://s3.ceph" |
.global.sync.destination.s3[0].endpoint = "https://s3.ceph"
' "$values_file"

  else
    ## S3 storage backend
    S3_ACCESS="$(random-string)"
    export S3_ACCESS
    S3_SECRET="$(random-string)"
    export S3_SECRET
    deploy_ceph_rgw false

    yq -i '
.global.archive.s3[0].endpoint = "http://s3.ceph" |
.global.backupArchive.s3[0].endpoint = "http://s3.ceph" |
.global.inbox.s3[0].endpoint = "http://s3.ceph" |
.global.s3Inbox.url = "http://s3.ceph" |
.global.sync.destination.s3[0].endpoint = "http://s3.ceph"
' "$values_file"

  fi
fi

PGPASSWORD="$(random-string)"
export PGPASSWORD

MQPASSWORD="$(random-string)"
export MQPASSWORD

TEST_TOKEN="$(bash .github/integration/scripts/sign_jwt.sh ES256 "$dir/jwt.key")"
export TEST_TOKEN

## update values file with all credentials
if [ "$2" == "federated" ]; then
        yq -i '.global.schemaType = "federated"' "$values_file"
fi

yq -i '
.global.archive.s3[0].accessKey = strenv(S3_ACCESS) |
.global.archive.s3[0].secretKey = strenv(S3_SECRET) |
.global.backupArchive.s3[0].accessKey = strenv(S3_ACCESS) |
.global.backupArchive.s3[0].secretKey = strenv(S3_SECRET) |
.global.broker.password = strenv(MQPASSWORD) |
.global.c4gh.privateKeys[0].passphrase = strenv(C4GHPASSPHRASE) |
.global.db.password = strenv(PGPASSWORD) |
.global.db.admin.password = strenv(PGPASSWORD) |
.global.inbox.s3[0].accessKey = strenv(S3_ACCESS) |
.global.inbox.s3[0].secretKey = strenv(S3_SECRET) |
.global.s3Inbox.accessKey = strenv(S3_ACCESS) |
.global.s3Inbox.secretKey = strenv(S3_SECRET) |
.global.sync.destination.s3[0].accessKey = strenv(S3_ACCESS) |
.global.sync.destination.s3[0].secretKey = strenv(S3_SECRET) |
.releasetest.secrets.accessToken = strenv(TEST_TOKEN)
' "$values_file"

kubectl create secret generic api-rbac --from-file=".github/integration/sda/rbac.json"

cat >/tmp/users.json <<EOD
[
    {
        "username": "dummy@example.com",
        "uid": 1,
        "passwordHash": "\$2b\$12\$1gyKIjBc9/cT0MYkXX24xe1LjEUjNwgL4rEk8fDoO.vDQZzWkqrn.",
        "gecos": "dummy user",
        "sshPublicKey": [],
        "enabled": null
    }
]
EOD
kubectl create configmap cega-nss --from-file=".github/integration/sda/users.py" --from-file="/tmp/users.json"