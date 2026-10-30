#!/bin/sh
# Single-container Ceph RGW for tests and local development.
#
# Starts a throwaway vstart cluster (one mon, one memstore OSD, no mgr) with
# RGW on port 9000, creates one S3 user and optionally some buckets. Data
# lives in memory and is gone when the container stops.
#
# Environment:
#   S3_ACCESS_KEY / S3_SECRET_KEY  credentials of the S3 user
#   S3_BUCKETS                     space separated buckets to create (optional)
#   RGW_TLS_CERT / RGW_TLS_KEY     serve https on port 9000 when both are set
#
# Health check: `/scripts/ceph-rgw.sh health` succeeds once setup is done and
# RGW still answers.
set -e

if [ "$1" = health ]; then
    ready_url=$(cat /tmp/rgw-ready 2>/dev/null) || exit 1
    exec curl -ksf -o /dev/null "$ready_url/swift/healthcheck"
fi

# A restarted container keeps /tmp but vstart --new rebuilds the cluster.
rm -f /tmp/rgw-ready

if [ -n "$RGW_TLS_CERT" ] && [ -n "$RGW_TLS_KEY" ]; then
    # vstart always appends "port=<rgw_port>", so plain http goes to 9001.
    frontend="beast ssl_port=9000 ssl_certificate=$RGW_TLS_CERT ssl_private_key=$RGW_TLS_KEY"
    rgw_port=9001
    url=https://localhost:9000
else
    frontend=beast
    rgw_port=9000
    url=http://localhost:9000
fi

# memstore reports 1 GiB of capacity by default and RGW answers 507 once the
# OSD is marked full, so raise what the OSD reports. Memory is only used for
# the objects actually stored.
cd /ceph
MON=1 MGR=0 OSD=1 RGW=1 ./vstart.sh --new --memstore --without-dashboard \
    -o "memstore_device_bytes = 4294967296" \
    --rgw_frontend "$frontend" --rgw_port "$rgw_port" >/tmp/vstart.log 2>&1 ||
    { cat /tmp/vstart.log; exit 1; }

radosgw-admin -c /etc/ceph/ceph.conf -k /etc/ceph/keyring user create \
    --uid=sda --display-name=sda \
    --access-key="$S3_ACCESS_KEY" --secret-key="$S3_SECRET_KEY" >/dev/null

until curl -ksf -o /dev/null "$url/swift/healthcheck"; do sleep 0.2; done

for bucket in $S3_BUCKETS; do
    python3 "$(dirname "$0")/s3.py" PUT "$url/$bucket"
done

echo "$url" > /tmp/rgw-ready
echo "Ceph RGW ready on $url"
exec tail -F /ceph/out/radosgw.*.log
