#!/bin/bash

if [ -n "$1" ]; then RELEASE_LIST=$1; else RELEASE_LIST="broker postgres sda"; fi

for release in $RELEASE_LIST; do
    echo "Testing $release"
    helm test "$release"
    r=$?

    if [ "$r" -ne 0 ]; then
        kubectl get pod -o name | while read -r pod; do
            echo "All logs for $pod"
            kubectl logs --all-containers "$pod"
        done
        exit "$r"
    fi
done
