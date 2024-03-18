#!/bin/bash

cd dev_utils || exit 1

cat << EOF > c4gh.pub.pem
-----BEGIN CRYPT4GH PUBLIC KEY-----
avFAerx0ZWuJE6fTI8S/0wv3yMo1n3SuNTV6zvKdxQc=
-----END CRYPT4GH PUBLIC KEY-----
EOF

chmod 444 c4gh.pub.pem

cat << EOF > c4gh.sec.pem
-----BEGIN CRYPT4GH ENCRYPTED PRIVATE KEY-----
YzRnaC12MQAGc2NyeXB0ABQAAAAAwAs5mVkXda50vqeYv6tbkQARY2hhY2hhMjBf
cG9seTEzMDUAPAd46aTuoVWAe+fMGl3VocCKCCWmgFUsFIHejJoWxNwy62c1L/Vc
R9haQsAPfJMLJSvUXStJ04cyZnDHSw==
-----END CRYPT4GH ENCRYPTED PRIVATE KEY-----
EOF

chmod 444 c4gh.sec.pem

cat << EOF > client.pub.pem
-----BEGIN CRYPT4GH PUBLIC KEY-----
ttQFo9vMkmVHcVwBWZysBWmxTiuE3mMQMeFBn+Zcuj0=
-----END CRYPT4GH PUBLIC KEY-----
EOF

chmod 444 client.pub.pem

cat << EOF > client.sec.pem
-----BEGIN CRYPT4GH ENCRYPTED PRIVATE KEY-----
YzRnaC12MQAGc2NyeXB0ABQAAAAAxNBT2Kb2TXyo3luxNCkbQAARY2hhY2hhMjBf
cG9seTEzMDUAPB0E9Gvhonx9xoL2sOGc214HRlpjlCROgRXI0sCPa5RKDLwjZxoo
b7RxX4aJqM0i0UaQg2aVSlat75GeoQ==
-----END CRYPT4GH ENCRYPTED PRIVATE KEY-----
EOF

chmod 444 client.sec.pem