#!/bin/bash
set -e

if [ "$1" = "standalone" ]; then
    INTERCEPT=false
    SCHEMA=isolated
else
    INTERCEPT=true
    CEGA_USERS_PASS=$(grep cega_users_pass dev_tools/config/cega.yaml | awk '{print $2}' | sed -e 's/\"//g')
    SCHEMA=federated
fi

DB_IN_PASS=$(grep pg_in_password sda-deploy-init/config/trace.yml | awk '{print $2}' | sed -e 's/\"//g')
DB_OUT_PASS=$(grep pg_out_password sda-deploy-init/config/trace.yml | awk '{print $2}' | sed -e 's/\"//g')
C4GH_PASSPHRASE=$(grep c4gh_passphrase sda-deploy-init/config/trace.yml | awk '{print $2}' | sed -e 's/\"//g')

if [ "$2" = "issuer" ]; then
    helm install sda charts/sda-svc -f dev_tools/config/posix.yaml \
        --set global.broker.vhost=/sda,global.c4gh.passphrase="$C4GH_PASSPHRASE",global.cega.password="$CEGA_USERS_PASS",global.db.passIngest="$DB_IN_PASS",global.db.passOutgest="$DB_OUT_PASS",global.schemaType="$SCHEMA",intercept.deploy="$INTERCEPT",global.tls.issuer=ca-issuer
else
    helm install sda charts/sda-svc -f dev_tools/config/posix.yaml \
        --set global.broker.vhost=/sda,global.c4gh.passphrase="$C4GH_PASSPHRASE",global.cega.password="$CEGA_USERS_PASS",global.db.passIngest="$DB_IN_PASS",global.db.passOutgest="$DB_OUT_PASS",global.schemaType="$SCHEMA",intercept.deploy="$INTERCEPT"
fi
