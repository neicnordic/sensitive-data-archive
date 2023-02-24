#!/bin/sh
set -e

if [ "$1" = "database" ]; then
    DB_IN_PASS=$(grep pg_in_password sda-deploy-init/config/trace.yml | awk '{print $2}' | sed -e 's/\"//g')
    DB_OUT_PASS=$(grep pg_out_password sda-deploy-init/config/trace.yml | awk '{print $2}' | sed -e 's/\"//g')

    helm install postgres charts/sda-db \
        --set global.pg_in_password="$DB_IN_PASS",global.pg_out_password="$DB_OUT_PASS",securityPolicy.create=false,global.tls.enabled=false,persistence.enabled=false

    RETRY_TIMES=0
    until kubectl get pods -l role=database -o jsonpath='{..status.containerStatuses[*].ready}' | grep "true"; do
        echo "waiting for database to become ready"
        RETRY_TIMES=$((RETRY_TIMES + 1))
        if [ "$RETRY_TIMES" -eq 30 ]; then
            kubectl describe pod -l role=datbase
            kubectl logs -l role=datbase
            exit 1
        fi
        sleep 10
    done
    exit 0
fi

if [ "$1" = "broker" ]; then
    HASH="$(/bin/sh dev_tools/scripts/mq-password-generator.sh admin)"
    helm install broker charts/sda-mq \
        --set securityPolicy.create=false,global.adminUser=admin,global.adminPasswordHash="$HASH",global.tls.enabled=false,global.vhost=sda,persistence.enabled=false

    RETRY_TIMES=0
    until kubectl get pods -l role=broker -o jsonpath='{..status.containerStatuses[*].ready}' | grep "true"; do
        echo "waiting for broker to become ready"
        RETRY_TIMES=$((RETRY_TIMES + 1))
        if [ "$RETRY_TIMES" -eq 30 ]; then
            kubectl describe pod -l role=broker
            kubectl logs -l role=broker
            exit 1
        fi
        sleep 10
    done
    exit 0
fi

if [ "$1" = "orchestrate" ]; then
    helm install orch charts/sda-orch -f dev_tools/config/orch.yaml \
        --set tls.enabled=false,broker.port=5672,broker.queue.verify=verified,broker.queue.inbox=files

    RETRY_TIMES=0
    until kubectl get pods -l role=orchestrate -o jsonpath='{..status.containerStatuses[*].ready}' | grep "true"; do
        echo "waiting for orch to become ready"
        RETRY_TIMES=$((RETRY_TIMES + 1))
        if [ "$RETRY_TIMES" -eq 30 ]; then
            kubectl describe pod -l role=orchestrate
            kubectl logs -l role=orchestrate
            exit 1
        fi
        sleep 10
    done
    exit 0
fi

if [ "$1" = "minio" ]; then
    helm repo add minio https://helm.min.io/
    helm repo update

    MINIO_ACCESS=$(grep s3_access_key sda-deploy-init/config/trace.yml | awk '{print $2}' | sed -e 's/\"//g')
    MINIO_SECRET=$(grep s3_secret_key sda-deploy-init/config/trace.yml | awk '{print $2}' | sed -e 's/\"//g')

    helm install minio minio/minio \
        --set accessKey="$MINIO_ACCESS",secretKey="$MINIO_SECRET",tls.enabled=false,persistence.enabled=false,service.port=9000 \
        --version 8.0.8

    RETRY_TIMES=0
    until kubectl get pods -l app=minio -o jsonpath='{..status.containerStatuses[*].ready}' | grep "true"; do
        echo "waiting for minio to become ready"
        RETRY_TIMES=$((RETRY_TIMES + 1))
        if [ "$RETRY_TIMES" -eq 30 ]; then
            kubectl describe pod -l app=minio
            kubectl logs -l app=minio
            exit 1
        fi
        sleep 10
    done
    exit 0
fi

if [ "$1" = "pipeline" ]; then
    DB_IN_PASS=$(grep pg_in_password sda-deploy-init/config/trace.yml | awk '{print $2}' | sed -e 's/\"//g')
    DB_OUT_PASS=$(grep pg_out_password sda-deploy-init/config/trace.yml | awk '{print $2}' | sed -e 's/\"//g')
    S3_ACCESS_KEY=$(grep s3_access_key sda-deploy-init/config/trace.yml | awk '{print $2}' | sed -e 's/\"//g')
    S3_SECRET_KEY=$(grep s3_secret_key sda-deploy-init/config/trace.yml | awk '{print $2}' | sed -e 's/\"//g')
    C4GH_PASSPHRASE=$(grep c4gh_passphrase sda-deploy-init/config/trace.yml | awk '{print $2}' | sed -e 's/\"//g')
    token="$(bash dev_tools/scripts/sign_jwt.sh ES256 sda-deploy-init/config/jwt.key)"
    helm install sda charts/sda-svc -f dev_tools/config/no-tls.yaml \
        --set global.archive.s3AccessKey="$S3_ACCESS_KEY",global.archive.s3SecretKey="$S3_SECRET_KEY",global.backupArchive.s3AccessKey="$S3_ACCESS_KEY",global.backupArchive.s3SecretKey="$S3_SECRET_KEY",global.broker.vhost=sda,global.c4gh.passphrase="$C4GH_PASSPHRASE",global.db.passIngest="$DB_IN_PASS",global.db.passOutgest="$DB_OUT_PASS",global.inbox.s3AccessKey="$S3_ACCESS_KEY",global.inbox.s3SecretKey="$S3_SECRET_KEY",releasetest.secrets.accessToken="$token"
fi
