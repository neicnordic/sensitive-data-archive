#!/usr/bin/env bash

set -e

[ "${BASH_VERSINFO[0]}" -lt 4 ] && echo 'Bash 4 (or higher) is required' 1>&2 && exit 1

if ! [ -x "$(command -v keytool)" ]; then
  echo 'Error: Keytool is not installed.' >&2
  exit 1
fi

if ! [ -x "$(command -v openssl)" ]; then
  echo 'Error: Openssl is not installed.' >&2
  exit 1
fi

CONFPATH="sda-deploy-init/config/certs"
STORETYPE=PKCS12
STOREPASS=changeit
services_input="doa,inbox"

IFS=',' read -r -a services <<< "$services_input"

# remove previous alias if keystore exists
# becomes problemantic if password changed
if [[ -f "${CONFPATH}"/cacerts ]]; then
    keytool -delete -alias legaCA \
            -keystore "${CONFPATH}"/cacerts \
            -storepass "${STOREPASS}" -noprompt
fi 

# create java keystore for each service
for service in "${services[@]}"; do
    if [[ "${STORETYPE}" == "JKS" ]]; then
        keytool -import -alias "${service}" \
                -keystore "${CONFPATH}/${service}.jks" \
                -file "${CONFPATH}/${service}".ca.crt.der \
                -storepass "${STOREPASS}" -noprompt
    else
        openssl pkcs12 -export -out "${CONFPATH}/${service}".p12 \
                       -inkey "${CONFPATH}/${service}".key \
                       -in "${CONFPATH}/${service}".crt \
                       -passout pass:"${STOREPASS}"
        openssl pkcs8 -topk8 \
                -inform pem \
                -outform der \
                -in "${CONFPATH}/${service}".key \
                -out "${CONFPATH}/${service}".key.der \
                -nocrypt
    fi
done 

# create java CAroot truststore
keytool -import -trustcacerts -file "${CONFPATH}"/ca.crt \
        -alias legaCA -storetype JKS \
        -keystore "${CONFPATH}"/cacerts \
        -storepass "${STOREPASS}" -noprompt

# create DER format key

