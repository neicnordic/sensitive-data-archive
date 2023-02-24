#!/bin/bash
set -e

if [ "$1" = "federated" ]; then
    CEGA_MQ_PASS=lega
fi

HASH="DI0kJIvQHptGSBH2coZ25dsjjN9Z4uxp8hAyqtd9H7rb/SBO"

if [ "$2" = "issuer" ]; then
    helm install broker charts/sda-mq \
        --set securityPolicy.create=false,global.adminUser=admin,global.adminPasswordHash="$HASH",global.shovel.host=cega-mq,global.shovel.user=lega,global.shovel.pass="$CEGA_MQ_PASS",global.shovel.vhost=lega,global.vhost=sda,global.tls.issuer=ca-issuer,global.tls.verifyPeer=true
else

    ## sda-mq certs
    kubectl create secret generic mq-certs \
        --from-file=sda-deploy-init/config/certs/ca.crt \
        --from-file=sda-deploy-init/config/certs/server.crt \
        --from-file=sda-deploy-init/config/certs/server.key

    kubectl create secret generic mq-tester-certs \
        --from-file=sda-deploy-init/config/certs/ca.crt \
        --from-file=sda-deploy-init/config/certs/tester.crt \
        --from-file=sda-deploy-init/config/certs/tester.key

    helm install broker charts/sda-mq \
        --set securityPolicy.create=false,global.adminUser=admin,global.adminPasswordHash="$HASH",global.shovel.host=cega-mq,global.shovel.user=lega,global.shovel.pass="$CEGA_MQ_PASS",global.shovel.vhost=lega,global.vhost=sda,global.tls.secretName=mq-certs,global.tls.keyName=server.key,global.tls.certName=server.crt,global.tls.caCert=ca.crt,global.tls.verifyPeer=true,testimage.tls.secretName=mq-tester-certs,testimage.tls.tlsKey=tester.key,testimage.tls.tlsCert=tester.crt,testimage.tls.caCert=ca.crt
fi
