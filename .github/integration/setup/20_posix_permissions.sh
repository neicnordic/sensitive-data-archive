#!/bin/bash

docker run --rm -v integration_archive:/foo alpine sh -c "chmod 777 /foo"
docker run --rm -v integration_backup:/foo alpine sh -c "chmod 777 /foo"
docker run --rm -v integration_inbox:/foo alpine sh -c "chmod 777 /foo"
