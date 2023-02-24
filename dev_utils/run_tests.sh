#!/bin/bash

#
cd "$(dirname "$0")" || exit

docker-compose -f compose-sda.yml down -v --remove-orphans
docker-compose -f compose-backend.yml down -v --remove-orphans

cd ..

tmpdir=$(mktemp -d)

# Set up a scratch python environment to not pollute anything else
python3 -m venv "$tmpdir"
# shellcheck disable=SC1091
. "$tmpdir/bin/activate"

export STORAGETYPE
for storage in s3 posix; do
    git checkout dev_utils/env.*

    STORAGETYPE=$storage

    for runscript in $(find . -maxdepth 1 -name ".github/integration/setup/{common,${storage}}/*.sh" 2>/dev/null | sort -t/ -k5 -n); do
	echo "Executing setup script $runscript";
	if bash -x "$runscript"; then
	    :
	else
	    echo "Setup failed"
	    rm -rf "$tmpdir"
	    exit 1
	fi
    done
    
    for runscript in $(find . -maxdepth 1 -name ".github/integration/tests/{common,${storage}}/*.sh" 2>/dev/null | sort -t/ -k5 -n); do
	echo "Executing test script $runscript";
	if bash -x "$runscript"; then
	    :
	else
	    echo "$runscript failed, bailing out"
	    rm -rf "$tmpdir"
	    exit 1
	fi
    done
done


