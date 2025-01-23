#!/bin/sh

mkdir -p ./test/crypt4gh

cat << 'EOF' > ./test/crypt4gh/crypt4gh.pub.pem
-----BEGIN CRYPT4GH PUBLIC KEY-----
PQFOuEiuAOlghTGU0Z7u2EH5FwBtSlnukpiMAji/ml0=
-----END CRYPT4GH PUBLIC KEY-----
EOF

chmod 444 ./test/crypt4gh/crypt4gh.pub.pem

cat << 'EOF' > ./test/crypt4gh/my.pub.pem
-----BEGIN CRYPT4GH PUBLIC KEY-----
cUSN6pHzlIFgoclIfDSaDtUgAXRa+DUHBhTodeNL52w=
-----END CRYPT4GH PUBLIC KEY-----
EOF

chmod 444 ./test/crypt4gh/my.pub.pem

cat << 'EOF' > ./test/crypt4gh/my.sec.pem
-----BEGIN CRYPT4GH ENCRYPTED PRIVATE KEY-----
YzRnaC12MQAGc2NyeXB0ABQAAAAAOK6Q2g1KxcUELMt/RMhr9wARY2hhY2hhMjBf
cG9seTEzMDUAPL3l1Mt/LvDwD+yffT09Jog2AJO3uG0DaGAPDTPbOfTBKr/gWRik
PcF6893CkScij1pO3n9Ub8p1H4yLAQ==
-----END CRYPT4GH ENCRYPTED PRIVATE KEY-----
EOF

chmod 444 ./test/crypt4gh/my.sec.pem

cat << 'EOF' > ./test/crypt4gh/crypt4gh.sec.pem
-----BEGIN CRYPT4GH ENCRYPTED PRIVATE KEY-----
YzRnaC12MQAGc2NyeXB0ABQAAAAAr3MTvNgHj/z6U02GqdILFwARY2hhY2hhMjBf
cG9seTEzMDUAPNzNGWSc7hWSxjwfuQJt2haq0/eyvoFjQXsvp+RCvXSEVgqlO58J
kgjKQgpRb9qm09AGhYU4tbXg7pyCRg==
-----END CRYPT4GH ENCRYPTED PRIVATE KEY-----
EOF

chmod 444 ./test/crypt4gh/crypt4gh.sec.pem

echo "CRYPT4GH files created successfully"
