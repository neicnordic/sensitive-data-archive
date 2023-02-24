#!/bin/bash

SALT=$(od -A n -t x -N 4 /dev/urandom)
PASS=$SALT$(echo -n "$1" | xxd -ps | tr -d '\n' | tr -d ' ')
PASS=$(echo -n "$PASS" | xxd -r -p | sha256sum | head -c 128)
PASS=$(echo -n "$SALT$PASS" | xxd -r -p | base64 -w0)
echo -n "$PASS"
