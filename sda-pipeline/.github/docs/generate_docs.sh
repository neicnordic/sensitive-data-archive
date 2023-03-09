#!/usr/bin/env bash

# Adapted from https://gitlab.com/-/snippets/1977141

function extract_module_name {
    # Extract module name
    sed -n -E 's/^\s*module\s+([[:graph:]]+)\s*$/\1/p'
}

function normalize_url {
    # Normalize provided URL. Removing double slashes
    echo "$1" | sed -E 's,([^:]/)/+,\1,g'
}

function cleanup {
    find "${GO_DOC_HTML_OUTPUT:-godoc}" -name 'index.html?m=all.html' | while read -r p; do
        mv "$p" "${p%?m=all.html}"
    done

    find "${GO_DOC_HTML_OUTPUT:-godoc}" -type f -print0 | xargs -0 sed -i -e 's/%3Fm=all.html//g'

    # this is for deep nested links
    find "${GO_DOC_HTML_OUTPUT:-godoc}" -type f -print0 | xargs -0 sed -i -e 's/..\/..\/..\/..\/src\/sda-pipeline\/internal/https:\/\/github.com\/neicnordic\/sda-pipeline\/tree\/master\/internal/g'

    find "${GO_DOC_HTML_OUTPUT:-godoc}" -type f -print0 | xargs -0 sed -i -e 's/..\/..\/..\/src\/sda-pipeline\/internal/https:\/\/github.com\/neicnordic\/sda-pipeline\/tree\/master\/internal/g'

    find "${GO_DOC_HTML_OUTPUT:-godoc}" -type f -print0 | xargs -0 sed -i -e 's/%3Fs=[[:digit:]]*:[[:digit:]]*.html//g'

    find "${GO_DOC_HTML_OUTPUT:-godoc}" -type f -print0 | xargs -0 sed -i -e 's/http:\/\/localhost:6060\/pkg/https:\/\/pkg.go.dev/g'

}

function generate_gh_pages {
    mkdir -p "${GITHUB_WORKSPACE}/gh-pages/pkg/sda-pipeline/"
    rsync -av "${GO_DOC_HTML_OUTPUT:-godoc}"/pkg/sda-pipeline/ "${GITHUB_WORKSPACE}/gh-pages/pkg/sda-pipeline/"
    rsync -av "${GO_DOC_HTML_OUTPUT:-godoc}"/lib/ "${GITHUB_WORKSPACE}/gh-pages/lib/"
}


function generate_go_documentation {
    # Go doc
    local URL
    local PID

    # Setup
    rm -rf "${GO_DOC_HTML_OUTPUT:-godoc}"

    # Extract Go module name from a Go module file
    if [[ -z "$GO_MODULE" ]]; then
        local FILE

        FILE="$(go env GOMOD)"

        if [[ -f "$FILE" ]]; then
            GO_MODULE=$(< "${FILE}" extract_module_name)
        fi
    fi

    # URL path to Go package and module documentation
    URL=$(normalize_url "http://${GO_DOC_HTTP:-localhost:6060}/pkg/$GO_MODULE/")

    # Starting godoc server
    echo "Starting godoc server..."
    godoc -http="${GO_DOC_HTTP:-localhost:6060}" -templates=.github/docs/godoc_templates &
    PID=$!

    # Waiting for godoc server
    while ! curl --fail --silent "$URL" > /dev/null 2>&1; do
        sleep 0.1
    done

    # Download all documentation content from running godoc server
    wget \
        --recursive \
        --no-verbose \
        --convert-links \
        --page-requisites \
        --adjust-extension \
        --execute=robots=off \
        --include-directories="/lib,/pkg/$GO_MODULE,/src/$GO_MODULE" \
        --exclude-directories="*" \
        --directory-prefix="${GO_DOC_HTML_OUTPUT:-godoc}" \
        --no-host-directories \
        "$URL?m=all"

    echo "cleaning up downloaded pages from suffix"
    cleanup

    echo "generate github pages"
    generate_gh_pages

    # Stop godoc server
    kill -9 "$PID"
    echo "Stopped godoc server"
    echo "Go source code documentation generated under ${GO_DOC_HTML_OUTPUT:-godoc}"
    
}


generate_go_documentation