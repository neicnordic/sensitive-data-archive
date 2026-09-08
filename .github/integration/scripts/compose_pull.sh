#!/bin/sh
# Pre-pull the images of a compose file, retrying on failure, so that a
# transient registry error does not fail an integration test job.
# All profiles are enabled so that the test runner service, which sits under
# a profile, is included, and buildable services are not skipped because
# `docker compose run` pulls their PR-tagged images rather than building them.
# Best effort: if every attempt fails, `docker compose run` still pulls
# whatever is missing and reports the error.
#
# Usage: compose_pull.sh <compose-file>
set -u

compose_file="$1"

for attempt in 1 2 3; do
    if docker compose -f "$compose_file" --profile '*' pull --quiet; then
        exit 0
    fi
    echo "::warning::pulling images for $compose_file failed, attempt $attempt of 3"
    sleep 30
done

echo "::warning::giving up on pre-pulling images for $compose_file"
