#!/bin/sh

if [ ! -s "${KEYSTORE_PATH}" ]; then
        echo "Creating PKCS12 keystore"
        openssl pkcs12 -export -out "${PKI_PATH}/doa.p12" \
                -inkey "${PKI_PATH}/doa.key" \
                -in "${PKI_PATH}/doa.crt" \
                -passout pass:"${KEYSTORE_PASSWORD}"
        echo "Creating DER key"
        openssl pkcs8 -topk8 \
                -inform pem \
                -outform der \
                -in "${PKI_PATH}/doa.key" \
                -out "${PKI_PATH}/doa.der" \
                -nocrypt
fi

exec "$@"
