#!/bin/sh
export USR=$(id -u)
export GRP=$(id -g)
cd testing/ || true
docker-compose up -d
