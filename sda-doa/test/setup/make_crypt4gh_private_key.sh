#!/bin/sh
docker exec -it --user root  doa sh -c '
mkdir -p test && \
cat << EOF > test/crypt4gh.sec.pem
-----BEGIN CRYPT4GH ENCRYPTED PRIVATE KEY-----
YzRnaC12MQAGc2NyeXB0ABQAAAAAr3MTvNgHj/z6U02GqdILFwARY2hhY2hhMjBf
cG9seTEzMDUAPNzNGWSc7hWSxjwfuQJt2haq0/eyvoFjQXsvp+RCvXSEVgqlO58J
kgjKQgpRb9qm09AGhYU4tbXg7pyCRg==
-----END CRYPT4GH ENCRYPTED PRIVATE KEY-----
EOF

chmod 444 test/crypt4gh.sec.pem

printf "password" > test/crypt4gh.pass

chmod 444 test/crypt4gh.pass

echo "CRYPT4GH private key created successfully"
' 2>/dev/null
