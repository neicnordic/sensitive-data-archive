#!/bin/bash

# Build containers
docker build -t neicnordic/sda-pipeline:latest . || exit 1

cd dev_utils || exit 1

if [ "$STORAGETYPE" = s3notls ]; then
    docker-compose -f compose-no-tls.yml up -d

    RETRY_TIMES=0
    for p in db mq s3 ingest verify finalize mapper intercept; do
        until docker ps -f name="$p" --format "{{.Status}}" | grep "Up"
        do echo "waiting for $p to become ready"
            RETRY_TIMES=$((RETRY_TIMES+1));
            if [ "$RETRY_TIMES" -eq 30 ]; then
                # Time out
                docker logs "$p"
                exit 1;
            fi
            sleep 10
        done
    done

elif [ "$STORAGETYPE" = s3notlsheader ]; then
    sed -i 's/copyHeader: "false"/copyHeader: "true"/g' config-notls.yaml

    docker-compose -f compose-no-tls.yml up -d

    RETRY_TIMES=0
    for p in db mq s3 ingest verify finalize mapper intercept; do
        until docker ps -f name="$p" --format "{{.Status}}" | grep "Up"
        do echo "waiting for $p to become ready"
            RETRY_TIMES=$((RETRY_TIMES+1));
            if [ "$RETRY_TIMES" -eq 30 ]; then
                # Time out
                docker logs "$p"
                exit 1;
            fi
            sleep 10
        done
    done

    sed -i 's/copyHeader: "true"/copyHeader: "false"/g' config-notls.yaml

else
    bash ./make_certs.sh

    tostart="certfixer db mq"

    if [ "$STORAGETYPE" = s3 ] || [ "$STORAGETYPE" = sftp ]; then
        tostart="certfixer db mq s3"
    elif [ "$STORAGETYPE" = s3header ] || [ "$STORAGETYPE" = posixheader ] || [ "$STORAGETYPE" = sftpheader ]; then
        tostart="certfixer db mq s3"
        sed -i 's/copyHeader: "false"/copyHeader: "true"/g' config.yaml
    fi

    # We need to leave the $tostart variable unquoted here since we want it to split
    # shellcheck disable=SC2086
    docker-compose -f compose-sda.yml up -d $tostart

    for p in $tostart; do
        RETRY_TIMES=0
        if [ "$p" = "certfixer" ]; then
            docker logs "$p"
            continue
        fi
        until docker ps -f name="$p" --format "{{.Status}}" | grep "(healthy)"
        do echo "waiting for $p to become ready"
            RETRY_TIMES=$((RETRY_TIMES+1));
            if [ "$RETRY_TIMES" -eq 30 ]; then
            # Time out
            docker logs "$p"
                exit 1;
                fi
            sleep 10
        done
    done

    docker-compose -f compose-sda.yml up -d

    for p in ingest verify finalize mapper intercept backup; do
        RETRY_TIMES=0
        until docker ps -f name="$p" --format "{{.Status}}" | grep "Up"
        do echo "waiting for $p to become ready"
            RETRY_TIMES=$((RETRY_TIMES+1));
            if [ "$RETRY_TIMES" -eq 30 ]; then
                # Time out
                docker logs "$p"
                exit 1;
            fi
            sleep 10
        done
    done

    if [ "$STORAGETYPE" = s3header ] || [ "$STORAGETYPE" = posixheader ] || [ "$STORAGETYPE" = sftpheader ]; then
        sed -i 's/copyHeader: "true"/copyHeader: "false"/g' config.yaml
    fi

fi

# Show running containers
docker ps

