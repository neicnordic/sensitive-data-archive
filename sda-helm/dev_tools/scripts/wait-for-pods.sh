#!/bin/bash
set -e

base_list="backup download finalize inbox ingest mapper verify"

if [ -n "$1" ]; then
  case "$1" in
  federated_s3_svc_list)
  SVCNAME="$base_list auth intercept"
  ;;

  federated_posix_svc_list)
  SVCNAME="$base_list intercept"
  ;;

  standalone_s3_svc_list)
  SVCNAME="$base_list auth"
  ;;

  standalone_posix_svc_list)
  SVCNAME="$base_list"
  ;;
  esac
fi

if [ -n "$2" ]; then
  LABEL=$2
else
  LABEL="role"
fi
if [ -n "$3" ]; then
  NAMESPACE=${3:-default}
fi

for p in $SVCNAME; do
  RETRY_TIMES=0
  until kubectl get pods -n "$NAMESPACE" -l "$LABEL=$p" -o jsonpath='{..status.containerStatuses[*].ready}' | grep "true"; do
    echo "waiting for $p to become ready"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
      kubectl describe pod -n "$NAMESPACE" -l "$LABEL"="$p"
      kubectl logs -n "$NAMESPACE" -l "$LABEL=$p"
      exit 1
    fi
    sleep 10
  done
done
