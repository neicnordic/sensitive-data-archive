#!/bin/sh
set -e

apt-get -o DPkg::Lock::Timeout=60 update > /dev/null
apt-get -o DPkg::Lock::Timeout=60 install -y postgresql-client > /dev/null

find "$1"/*.sh 2>/dev/null |  sort -t/ -k3 -n | while read -r runscript; do
    echo "Executing test script $runscript"
    bash -x "$runscript"
done
