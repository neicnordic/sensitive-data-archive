#!/bin/bash

cd dev_utils || exit 1

touch largefile.raw
size=$(shuf -i 300-600 -n 1)
shred -n 1 -s "$size"M largefile.raw

md5sum largefile.raw > largefile.raw.md5
sha256sum largefile.raw > largefile.raw.sha256

crypt4gh encrypt --recipient_pk c4gh.pub.pem < largefile.raw > largefile.c4gh

rm largefile.raw

touch empty.c4gh

dd if=largefile.c4gh bs=1 count="$((15000+RANDOM))" of=truncated1.c4gh
dd if=largefile.c4gh bs=1 count=10 of=truncated2.c4gh

dd if=/dev/random of=wrongly_encrypted.raw count=1 bs=$(( 1024 * 1024 *  1 )) iflag=fullblock

/usr/bin/expect <<EOD
spawn crypt4gh-keygen --sk wrong_key.key --pk wrong_key.pub
match_max 100000
expect -exact "Generating public/private Crypt4GH key pair.\r
Enter passphrase for wrong_key.key (empty for no passphrase): "
send -- "secret\r"
expect -exact "\r
Enter passphrase for wrong_key.key (again): "
send -- "secret\r"
expect eof
EOD

crypt4gh encrypt --recipient_pk wrong_key.pub < wrongly_encrypted.raw > wrongly_encrypted.c4gh
rm -f wrongly_encrypted.raw wrong_key.pub wrong_key.key

touch test_db_file.raw
shred -n 1 -s "$RANDOM" test_db_file.raw

crypt4gh encrypt --recipient_pk c4gh.pub.pem < test_db_file.raw > test_db_file.c4gh
rm test_db_file.raw