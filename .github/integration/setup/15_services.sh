#!/bin/bash

cd .github/integration || exit 1

docker-compose up -d cegamq certfixer db mq

for p in cegamq db mq; do
    RETRY_TIMES=0
    until docker ps -f name="$p" --format "{{.Status}}" | grep "(healthy)"; do
        echo "waiting for $p to become ready"
        RETRY_TIMES=$((RETRY_TIMES + 1))
        if [ "$RETRY_TIMES" -eq 30 ]; then
            # Time out
            docker logs "$p"
            exit 1
        fi
        sleep 10
    done
done

docker-compose up -d

for p in ingest intercept finalize mapper sync verify; do
    RETRY_TIMES=0
    until docker ps -f name="$p" --format "{{.Status}}" | grep "Up"; do
        echo "waiting for $p to become ready"
        RETRY_TIMES=$((RETRY_TIMES + 1))
        if [ "$RETRY_TIMES" -eq 30 ]; then
            # Time out
            docker logs "$p"
            exit 1
        fi
        sleep 10
    done
done

# Show running containers
docker ps -a
