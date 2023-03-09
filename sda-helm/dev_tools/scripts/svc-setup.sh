#!/bin/bash
set -e

basedir="sda-deploy-init/config"

## cega config and certs
mkdir -p LocalEGA-helm/ega-charts/cega/config/certs
cp -r dev_tools/cega/* LocalEGA-helm/ega-charts/cega/config/
cp "${basedir}"/certs/ca.crt LocalEGA-helm/ega-charts/cega/config/certs/ca.crt
cp "${basedir}"/certs/cega.crt LocalEGA-helm/ega-charts/cega/config/certs/tls.crt
cp "${basedir}"/certs/cega.key LocalEGA-helm/ega-charts/cega/config/certs/tls.key

## sda-svc certs

for n in backup doa finalize inbox ingest intercept verify mapper auth tester download
  do
  kubectl create secret generic $n-certs \
  --from-file="${basedir}"/certs/ca.crt \
  --from-file=tls.crt="${basedir}"/certs/$n.crt \
  --from-file=tls.key="${basedir}"/certs/$n.key
done
