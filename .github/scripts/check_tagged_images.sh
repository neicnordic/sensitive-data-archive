#!/bin/bash
# Check that every tagged version of the ghcr package still pulls: its manifest
# resolves and, for an image index, every child manifest resolves too.
# A broken version with a semantic version tag fails the script; other broken
# versions (PR images) only give a warning.
# Usage: check_tagged_images.sh <org> <package>   (needs gh with read:packages)
set -euo pipefail

org=$1
package=$2
semver='^v?[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$'
# Broken before this check existed; their children were deleted in March 2026
# and are past GitHub's 30-day restore window.
known_broken='^v3\.1\.2[67]([.-]|$)'

work=$(mktemp -d)
list_once() {
    gh api "/orgs/$org/packages/container/$package/versions?per_page=100" --paginate \
        --jq '.[] | select(.metadata.container.tags | length > 0) | "\(.name) \(.metadata.container.tags | join(","))"' | sort -u
}
# The API pages by offset, so a version added or deleted while listing can make
# another one fall between two pages. Only trust a listing that the next one repeats.
a=$(list_once)
stable=false
for _ in 1 2 3 4 5; do
    b=$(list_once)
    if [ "$a" = "$b" ]; then
        stable=true
        break
    fi
    a=$b
done
if [ "$stable" != true ]; then
    echo "::error::The package kept changing while it was listed"
    exit 1
fi
echo "$b" > "$work/tagged"

TOKEN=$(curl -sf --retry 5 --retry-all-errors --retry-delay 5 "https://ghcr.io/token?scope=repository:$org/$package:pull" | jq -er .token)
export TOKEN REPO="$org/$package"
export ACCEPT="application/vnd.oci.image.index.v1+json,application/vnd.docker.distribution.manifest.list.v2+json,application/vnd.docker.distribution.manifest.v2+json,application/vnd.oci.image.manifest.v1+json"

# Child digests of exactly one well-formed manifest; anything else is an error.
export CHILD_DIGESTS='[inputs]
    | if length == 1 and (.[0] | type == "object" and (.schemaVersion | type == "number") and ((has("manifests") | not) or (.manifests | type == "array")))
      then .[0] | (if has("manifests") then .manifests[] else empty end) | if type == "object" and (.digest | type == "string") then .digest else error("bad child") end
      else error("not a manifest") end'

# Fails when the manifest, or any manifest it points to, does not resolve.
# Stops at a depth of four, which also ends a reference cycle.
# shellcheck disable=SC2329 # called through check below
pulls() {
    local digest=$1 depth=$2 out code kids child
    [ "$depth" -le 4 ] || return 1
    # Without -f a 404 is an answer, not an error, so curl does not retry it.
    out=$(mktemp)
    code=$(curl -s --retry 10 --retry-all-errors --retry-delay 5 -o "$out" -w '%{http_code}' \
        -H "Authorization: Bearer $TOKEN" -H "Accept: $ACCEPT" "https://ghcr.io/v2/$REPO/manifests/$digest") || code=failed
    if [ "$code" != 200 ] || ! kids=$(jq -rn "$CHILD_DIGESTS" < "$out"); then
        rm -f "$out"
        return 1
    fi
    rm -f "$out"
    for child in $kids; do
        pulls "$child" $((depth + 1)) || return 1
    done
}
# Prints "<digest> <tags>" when the version does not pull.
# shellcheck disable=SC2329 # called through xargs below
check() {
    pulls "$1" 0 || echo "$1 $2"
}
export -f pulls check

# shellcheck disable=SC2016 # $1 and $2 belong to the inner bash
xargs -r -P 4 -L 1 bash -c 'check "$1" "$2"' _ < "$work/tagged" > "$work/broken"

echo "Checked $(wc -l < "$work/tagged") tagged versions, $(wc -l < "$work/broken") do not pull."
failed=0
while read -r digest tags; do
    if tr ',' '\n' <<<"$tags" | grep -E "$semver" | grep -Evq "$known_broken"; then
        echo "::error::Release image does not pull: $tags ($digest)"
        failed=1
    elif tr ',' '\n' <<<"$tags" | grep -Eq "$known_broken"; then
        echo "Known broken: $tags ($digest)"
    else
        echo "::warning::Image does not pull: $tags ($digest)"
    fi
done < "$work/broken"
exit "$failed"
