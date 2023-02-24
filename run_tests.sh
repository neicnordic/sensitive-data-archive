#!/bin/sh

USR="$(id -u)"
GRP="$(id -g)"

export USR
export GRP

cd "$(dirname "$0")/testing" || exit 1

docker compose run tests
docker compose down --volumes
