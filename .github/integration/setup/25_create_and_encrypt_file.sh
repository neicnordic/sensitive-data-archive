#!/bin/bash

cd .github/integration || exit 1

touch file.raw
size=$(echo "$RANDOM" '*' "$RANDOM" | bc)
shred -n 1 -s "$size" file.raw

md5sum file.raw > file.raw.md5
sha256sum file.raw > file.raw.sha256
wc -c file.raw > file.raw.stats

yes | crypt4gh encrypt -p c4gh.pub.pem -f file.raw

rm file.raw
