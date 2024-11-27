#!/bin/sh
set -e

if [ -n "$SYNCTEST" ]; then
    exit 0
fi

python -m pip install --upgrade pip
pip install tox

tox -e unit_tests -c /tests/sda/auth/tox.ini

echo "auth test completes successfully"
