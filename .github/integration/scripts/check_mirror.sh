#!/bin/bash
# Check that the integration stacks only pull third-party images from the CI mirror,
# and that every mirror tag they use is listed in mirror-images.txt.
#
# Usage: check_mirror.sh (from the repository root)
set -euo pipefail
shopt -s inherit_errexit

mirror="ghcr.io/neicnordic/sda-ci-mirror"
list=".github/integration/mirror-images.txt"
# not mirrored: ls_aai_mock is outside the mirror's scope, sda-inbox-sftp is built locally
allowed="harbor.nbis.se/test/ls_aai_mock:.*|neicnordic/sda-inbox-sftp"

charts=.github/integration/scripts/charts
manifests=(.github/integration/*.yml sda-download/dev_utils/*.yml sda-sftp-inbox/dev_utils/docker-compose.yml
    "$charts/dependencies.yaml" "$charts/dependencies.sh")
mapfile -t scripts < <(grep -rl 'docker run' sda-download/.github/integration)
[ "${#scripts[@]}" -gt 0 ] || { echo "::error::no docker run scripts found"; exit 1; }

# yaml `image:` and json "image": values, Dockerfile FROM, and every image-like word in the docker run scripts
refs=$(
    sed -nE "s/^[[:space:]]*(- )?\"?image\"?:[[:space:]]*[\"']?([^\"'[:space:],#]+).*/\2/p" "${manifests[@]}"
    sed -nE 's/^FROM[[:space:]]+([^[:space:]]+).*/\1/p' "$charts/Dockerfile"
    cat "${scripts[@]}" | tr -s " \t\"'\\\\" '\n' | { grep -E '^([a-z0-9.-]+/)*[a-z0-9._-]+(:[A-Za-z0-9._-]+|@sha256:[0-9a-f]+)$' || true; }
)

bad=()
while read -r ref; do
    case "$ref" in
    "$mirror":*)
        awk -v t="${ref#"$mirror":}" '!/^#/ && $2 == t { found = 1 } END { exit !found }' "$list" ||
            bad+=("$ref (not in $list)")
        ;;
    ghcr.io/neicnordic/sensitive-data-archive:* | ghcr.io/neicnordic/sda-mq:*) ;; # our own images
    *) [[ "$ref" =~ ^($allowed)$ ]] || bad+=("$ref (not mirrored)") ;;
    esac
done < <(sort -u <<<"$refs")

if [ "${#bad[@]}" -gt 0 ]; then
    printf '::error::%s\n' "${bad[@]}"
    exit 1
fi
echo "all images are mirrored"
