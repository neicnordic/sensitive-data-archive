#!/bin/bash
# Delete container versions of a ghcr package that are no longer needed:
#  1. tagged versions without a semantic version tag, last updated more than 30 days ago, and
#  2. untagged versions older than a day,
# unless a remaining tagged version points to them, directly or through nested
# indexes. An image index keeps its platform image and attestation as untagged
# versions on ghcr. The day of grace covers an image whose children are pushed
# but whose tagged index is not yet.
# Usage: clean_ghcr_images.sh <org> <package> [dry-run]   (needs gh with packages:write)
set -euo pipefail
shopt -s inherit_errexit

org=$1
package=$2
dry_run=${3:-false}
semver='^v?[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$'
api="/orgs/$org/packages/container/$package/versions"

work=${WORK_DIR:-$(mktemp -d)}
# Every version seen tagged in any listing during the run keeps protecting what it
# points to, even if a later listing misses it.
: > "$work/seen_tagged"
list_once() {
    gh api "$api?per_page=100" --paginate \
        --jq '.[] | {id, name, updated_at, tags: .metadata.container.tags}' | jq -s 'unique_by(.id)' > "$work/listing" || return 1
    jq -r '.[] | select(.tags | length > 0) | .name' "$work/listing" >> "$work/seen_tagged" || return 1
    cat "$work/listing"
}
# The API pages by offset, so a version added or deleted while listing can make
# another one fall between two pages. Only trust a listing that the next one repeats.
list_versions() {
    local a b
    a=$(list_once)
    for _ in 1 2 3 4 5; do
        b=$(list_once)
        if [ "$a" = "$b" ]; then
            echo "$b"
            return
        fi
        a=$b
    done
    echo "The package kept changing while it was listed" >&2
    return 1
}

# Record "<digest> <child digest>" for every manifest in an image index, and
# "<digest> -" for every manifest that was read.
TOKEN=$(curl -sf --retry 5 --retry-all-errors --retry-delay 5 "https://ghcr.io/token?scope=repository:$org/$package:pull" | jq -er .token)
export TOKEN REPO="$org/$package"
export ACCEPT="application/vnd.oci.image.index.v1+json,application/vnd.docker.distribution.manifest.list.v2+json,application/vnd.docker.distribution.manifest.v2+json,application/vnd.oci.image.manifest.v1+json"
# Child digests of exactly one well-formed manifest; anything else is an error.
export CHILD_DIGESTS='[inputs]
    | if length == 1 and (.[0] | type == "object" and (.schemaVersion | type == "number") and ((has("manifests") | not) or (.manifests | type == "array")))
      then .[0] | (if has("manifests") then .manifests[] else empty end) | if type == "object" and (.digest | type == "string") then .digest else error("bad child") end
      else error("not a manifest") end'
# shellcheck disable=SC2329 # called through xargs below
children() {
    local digest=$1 tagged=$2 out code kids kid
    # curl truncates an output file before each retry, so a failed attempt's body is not kept.
    out=$(mktemp)
    if ! code=$(curl -s --retry 10 --retry-all-errors --retry-delay 5 -o "$out" -w '%{http_code}' \
        -H "Authorization: Bearer $TOKEN" -H "Accept: $ACCEPT" "https://ghcr.io/v2/$REPO/manifests/$digest"); then
        echo "Fetching the manifest of $digest failed" >&2
        exit 255
    fi
    if [ "$code" = 404 ] && [ "$tagged" = false ]; then
        rm -f "$out"
        return # an untagged manifest that is already gone protects nothing
    fi
    if [ "$code" != 200 ] || ! kids=$(jq -rn "$CHILD_DIGESTS" < "$out"); then
        echo "Fetching the manifest of $digest failed with HTTP $code" >&2
        exit 255
    fi
    rm -f "$out"
    for kid in $kids; do
        echo "$digest $kid"
    done
    echo "$digest -"
}
export -f children
# Read the manifests of a listing's versions and of every version seen tagged,
# unless they were read before.
: > "$work/children"
fetch_missing() {
    { jq -r '.[] | "\(.name) \(.tags | length > 0)"' "$1"; sed 's/$/ true/' "$work/seen_tagged"; } | sort -u |
        awk 'FILENAME == ARGV[1] { if ($2 == "-") read[$1]; next } !($1 in read)' "$work/children" - > "$work/todo"
    # shellcheck disable=SC2016 # $1 and $2 belong to the inner bash
    xargs -r -P 4 -L 1 bash -c 'children "$1" "$2"' _ < "$work/todo" >> "$work/children"
}
list_versions > "$work/versions.json"
fetch_missing "$work/versions.json"
# Reading the manifests takes minutes. Decide on a listing taken after that, with
# the manifests of anything that appeared in the meantime.
list_versions > "$work/versions.json"
fetch_missing "$work/versions.json"

jq -r --arg semver "$semver" --rawfile edges "$work/children" --rawfile seen "$work/seen_tagged" '
    def set: map({key: ., value: true}) | from_entries;
    ($edges | split("\n") | map(select(length > 0) | split(" ") | select(.[1] != "-"))
        | group_by(.[0]) | map({key: .[0][0], value: map(.[1])}) | from_entries) as $adj
    | [.[] | select((.tags | length > 0)
        and ([.tags[] | test($semver)] | any | not)
        and (.updated_at | fromdateiso8601) < now - 30 * 86400)] as $old
    | ($old | map(.name) | set) as $old_names
    | ([.[] | select(.tags | length > 0) | .name] + ($seen | split("\n") | map(select(length > 0)))
        | unique | map(select($old_names[.] == null))) as $roots
    # Everything a remaining tagged version points to, directly or through nested indexes.
    | ({seen: ($roots | set), todo: $roots}
        | until(.todo | length == 0;
            .seen as $seen
            | ([.todo[] | ($adj[.] // [])[]] | unique | map(select($seen[.] == null))) as $new
            | .seen += ($new | set)
            | .todo = $new)
        | .seen) as $kept
    | ($old + [.[] | select((.tags | length == 0) and (.updated_at | fromdateiso8601) < now - 86400)])[]
    | select($kept[.name] == null)
    | "\(.id) \(.updated_at) \(.tags | join(","))"
' "$work/versions.json" > "$work/delete"

echo "$(jq length "$work/versions.json") versions, $(wc -l < "$work/delete") to delete."
while read -r id date tags; do
    if [ "$dry_run" = true ]; then
        echo "Would delete id: $id, last updated at: $date (tags: $tags)"
    else
        echo "Deleting id: $id, last updated at: $date (tags: $tags)"
        gh api -X DELETE "$api/$id" > /dev/null
    fi
done < "$work/delete"

[ "$dry_run" = true ] && exit 0
semver_ids() { jq -r --arg semver "$semver" '.[] | select([.tags[] | test($semver)] | any) | .id' "$1" | sort; }
list_versions > "$work/after.json"
semver_ids "$work/versions.json" > "$work/semver_before"
semver_ids "$work/after.json" > "$work/semver_after"
missing=$(comm -23 "$work/semver_before" "$work/semver_after" | tr '\n' ' ')
if [ -n "$missing" ]; then
    echo "::error::Versions with a semantic version tag disappeared: $missing"
    exit 1
fi
