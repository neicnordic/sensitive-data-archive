#!/bin/bash
set -e

DB_IN_PASS=$(grep pg_in_password sda-deploy-init/config/trace.yml | awk '{print $2}' | sed -e 's/\"//g')
DB_OUT_PASS=$(grep pg_out_password sda-deploy-init/config/trace.yml | awk '{print $2}' | sed -e 's/\"//g')

if [ "$1" = "issuer" ]; then
    helm install postgres charts/sda-db \
        --set securityPolicy.create=false,global.pg_in_password="$DB_IN_PASS",global.pg_out_password="$DB_OUT_PASS",global.tls.issuer=ca-issuer
else
    ## sda-db certs
    kubectl create secret generic db-certs \
        --from-file=ca.crt=sda-deploy-init/config/certs/ca.crt \
        --from-file=tls.crt=sda-deploy-init/config/certs/pg.crt \
        --from-file=tls.key=sda-deploy-init/config/certs/pg.key

    # tester certs
    kubectl create secret generic db-tester-certs \
        --from-file=ca.crt=sda-deploy-init/config/certs/ca.crt \
        --from-file=tls.crt=sda-deploy-init/config/certs/tester.crt \
        --from-file=tls.key=sda-deploy-init/config/certs/tester.key

    helm install postgres charts/sda-db \
        --set securityPolicy.create=false,global.pg_in_password="$DB_IN_PASS",global.pg_out_password="$DB_OUT_PASS",global.tls.secretName=db-certs,testimage.tls.secretName=db-tester-certs
fi
