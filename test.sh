#!/bin/bash
#
# convenience script to build and run database tests.
#

set -e

docker build -t sda-db-tests -f ./tests/Dockerfile .

docker run -t sda-db-tests

