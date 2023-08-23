#!/bin/bash
#
# This script is run in the "Aggregate documentation" github workflow.
#
# The script takes one or more repositories as arguments, clones those
# repositories main branches, and copies files into this repo according to the
# mappings in `aggregate-mappings.json`.
#

set -e

echo "Aggregating files"

for repo in "$@"
do
    # clone repo into a temp dir
    tempdir="$(mktemp -d)"
    git clone "https://github.com/$repo" "$tempdir/${repo#*/}"

    # get file mappings from mappings file
    repo_mappings=$(jq .["\"$repo\""] < aggregate-mappings.json)
    for key in $(jq -r 'keys[]' <<< "$repo_mappings")
    do
        target=$(jq -r .["\"$key\""] <<< "$repo_mappings")
        if [ -f "$tempdir/${repo#*/}/$key" ]
        then
            cp "$tempdir/${repo#*/}/$key" "$target"
            git add "$target"
        fi
    done

    # check if there are any changes
    if ! git status | grep 'nothing to commit'
    then
        # commit files to repo
        msg=$(date +"Update from $repo at %H:%M on %Y-%m-%d")

        git config --global user.name 'Github aggregate action'
        git config --global user.email 'neicnordic@users.noreply.github.com'
        git commit -m "$msg"
    else
        echo "No changes to commit"
    fi

    # clean up temp dir
    rm -rf "$tempdir"

done
