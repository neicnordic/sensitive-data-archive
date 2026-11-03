#!/bin/bash
# Print the state and the last log lines of every container, for a compose job that failed.
docker ps -a
for c in $(docker ps -aq); do
    echo "::group::$(docker inspect -f '{{.Name}} ({{.State.Status}}, exit {{.State.ExitCode}})' "$c")"
    docker logs --tail 200 "$c" 2>&1
    echo "::endgroup::"
done
