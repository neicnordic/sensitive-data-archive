#!/usr/bin/env sh

set -e

echo "Updating CA certificates..."
update-ca-certificates
echo "Done!"

exec "$@"
