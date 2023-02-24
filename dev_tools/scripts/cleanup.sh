#!/bin/sh

if [ "$1" = "db" ] || [ "$1" = "all" ];then
    helm uninstall postgres
    kubectl delete secret db-certs
fi

if [ "$1" = "mq" ] || [ "$1" = "all" ];then
    helm uninstall broker
    kubectl delete secret mq-certs
fi

if [ "$1" = "sda" ] || [ "$1" = "all" ];then
    helm uninstall sda
    kubectl delete secret auth-certs backup-certs doa-certs finalize-certs inbox-certs ingest-certs intercept-certs mapper-certs tester-certs verify-certs ca-root download-certs
fi
