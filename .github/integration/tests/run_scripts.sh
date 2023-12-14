#!/bin/sh
set -e

apt-get -o DPkg::Lock::Timeout=60 update > /dev/null
apt-get -o DPkg::Lock::Timeout=60 install -y postgresql-client > /dev/null

for runscript in "$1"/*.sh; do
    echo "Executing test script $runscript"
    bash "$runscript"
done
