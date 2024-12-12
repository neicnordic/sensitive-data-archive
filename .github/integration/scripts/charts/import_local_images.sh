#!/bin/bash
set -ex

if [ -z "$1" ]; then
    echo "cluster name not defined, exiting."
    exit 1
fi

for name in postgres rabbitmq download; do
    k3d image import "ghcr.io/neicnordic/sensitive-data-archive:PR$(date +%F)-$name" -c "$1"
done

k3d image import "ghcr.io/neicnordic/sensitive-data-archive:PR$(date +%F)" -c "$1"

docker build -t ghcr.io/neicnordic/sensitive-data-archive:oidc -f .github/integration/scripts/charts/Dockerfile .
k3d image import ghcr.io/neicnordic/sensitive-data-archive:oidc -c "$1"