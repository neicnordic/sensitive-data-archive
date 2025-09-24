#!/bin/sh
set -e

URI=http://rabbitmq:15672

# Postgres requires a client certificate, so this is a simple way of detecting if TLS is enabled or not.
if [ -n "$PGSSLCERT" ]; then
    URI=https://rabbitmq:15671
fi

apt-get -o DPkg::Lock::Timeout=60 update > /dev/null
apt-get -o DPkg::Lock::Timeout=60 install -y curl jq openssh-client openssl postgresql-client xxd >/dev/null

pip install --upgrade pip > /dev/null
pip install aiohttp Authlib joserfc requests > /dev/null

for n in api auth download finalize inbox ingest mapper rotatekey sync verify; do
    echo "creating credentials for: $n"
    psql -U postgres -h postgres -d sda -c "ALTER ROLE $n LOGIN PASSWORD '$n';"
    psql -U postgres -h postgres -d sda -c "GRANT base TO $n;"

    ## password and permissions for MQ
    body_data=$(jq -n -c --arg password "$n" --arg tags none '$ARGS.named')
    curl -s -u guest:guest -X PUT -k "$URI/api/users/$n" -H "content-type:application/json" -d "${body_data}"
    curl -s -u guest:guest -X PUT -k "$URI/api/permissions/sda/$n" -H "content-type:application/json" -d '{"configure":"","write":"sda","read":".*"}'
done

# create EC256 key for signing the JWT tokens
mkdir -p /shared/keys/pub
if [ ! -f "/shared/keys/jwt.key" ]; then
    echo "creating jwt key"
    cat << 'EOF' > /shared/keys/jwt.key
-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQDhuZjxPmOGUIW1
LhxzKfxkN+1aTbvI5w+AptqT33X+bWuzfjvhEodiNz0bBfQgJJpQ3TZ8J1IZpM2F
Tnzox+FGxKPe5T9Mgngzd4N6eByWVPXoNMk7IdmBXMdPZBFSyjMW4ba1MELCpiKV
05de4J5opRDwmHmyMqYJxBk78e3iiYYixVk+j1Ku+yFl4d2R29y2+O9PlZegJloe
8FGnKIGZApS/8t9iyCkXg8WbjSPzgYCTQKxn/E4lcGdTrAt/McKrWmAuppcr+rpP
+BInm3l5Zu/QiRSZcMb5O460ojP9eKnaUlDpGZv9CY5j4x4lq8vjU2kK77YXBO8I
2oxse5a5AgMBAAECggEABbwSX6anHqVzECxQurhJWj51gELTT4JXSXxztygJNmKP
RushGFHBMMSYf9RB5IMpjH5iQPs6wb4HHqjk0YEqfwLF6wbF+eqipSQXKghdKZCV
AsY8io0MmpXB1omDSygp7h3j52yHdayE2muav+VTAPOYn5QwG0/gGgVqYrR9x7CM
iTuyOIuGNO4Wlly4/5RhLtSo0pal9AgBvX4crtVEwN8tPgqPVo9w71bSROt9EVNI
3cZiFFrrapYiifckIGiPGQYQUd5ej9Mq/77Fa0fv0pk0ONQV8HwstQ5HY2WwJWsn
mccF9plVTzem7N/vo+T+hFRPUO9TZUao91mMV8iV5QKBgQD1nZbQW3NHdol0fXA8
nw5JRkTLZx1zcZ5l36WVPkwCjJOyXQ2vWHm4lz7F81Rr8dQnMKLWMDKjrBT9Dbfs
xYK2bYxENS1W/n+0jOIaX/792DY9tfX7vvHU9yGSdoJE5os6DGCHYInOD0xnRmnl
3vS7gKv8miDwDzFsbjtDg6WfSwKBgQDrRLkmmfZCMcmLA02YSrErAlUseuyad7lY
HEJApXKfn262iHELlQa2zOBZpJGXIcHsNf1XGpMeU5pH+ILKE4Y5qbclq+AzFCcZ
nBFUfDeawmWdV5FJqNDd1L8Mb8aE+6q0Y5rNb3RL7A2ypH2ZeYKSGpHz3C7Rn5KW
voWAXRWriwKBgQCH4bxK3x0ivxiCgtcyIojDzwVGRnDLqmMIVzeDHqjsjBs2BTcJ
9/e3QK1w1BKzeWF2oPilaJrLY+tkqE9FxWtwQ6DjJ0xDIZ9DIuH/13X5t8EiWOWS
devSdzpyje+58JW78pcArk7u2hXZ2OHDU5qvlRsRL6/jP3SHWWCeFFnviwKBgGov
M02r0YygwfEfBYeFtp7Nx7lypZU2Eg4levWIdsp6f9KclEEA+u3IXD25XAiVMNw2
pegJU3stioWPMSCZXUxrQAEdqOwE3XzehqfWBJaxxIEWQ7m2Gsb0PWIUlMnyeGJA
Tl8IPboCiVAmk5WQVREyMsuYhf0Qg23MAZ8k5CHvAoGBAJm55NQZVKAEDGd4a21q
TDcRddtPwwL2oP3qa0gbGk4YFRUCrX99hIejOTvQW1xf6vGxTd7E1QizvFse4yRz
ZRKyXIc7DCcdzOnpMrSd1+aXwZtRHLSw0EDS6PWeJZdjJYHxl2YpAmMdURdcGTrH
b6b/6vhU90+xL14CX7Awofp/
-----END PRIVATE KEY-----
EOF
    cat << 'EOF' > /shared/keys/pub/jwt.pub
-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA4bmY8T5jhlCFtS4ccyn8
ZDftWk27yOcPgKbak991/m1rs3474RKHYjc9GwX0ICSaUN02fCdSGaTNhU586Mfh
RsSj3uU/TIJ4M3eDengcllT16DTJOyHZgVzHT2QRUsozFuG2tTBCwqYildOXXuCe
aKUQ8Jh5sjKmCcQZO/Ht4omGIsVZPo9SrvshZeHdkdvctvjvT5WXoCZaHvBRpyiB
mQKUv/LfYsgpF4PFm40j84GAk0CsZ/xOJXBnU6wLfzHCq1pgLqaXK/q6T/gSJ5t5
eWbv0IkUmXDG+TuOtKIz/Xip2lJQ6Rmb/QmOY+MeJavL41NpCu+2FwTvCNqMbHuW
uQIDAQAB
-----END PUBLIC KEY-----
EOF
    chmod 644 /shared/keys/pub/jwt.pub /shared/keys/jwt.key
fi

echo "creating token"
python /scripts/sign_jwt.py testu@lifescience-ri.eu  > "/shared/token"

cat >/shared/s3cfg <<EOD
[default]
access_key=test@dummy.org
secret_key=test@dummy.org
access_token="$(python /scripts/sign_jwt.py test@dummy.org)"
check_ssl_certificate = False
check_ssl_hostname = False
encoding = UTF-8
encrypt = False
guess_mime_type = True
host_base = s3inbox:8000
host_bucket = s3inbox:8000
human_readable_sizes = true
multipart_chunk_size_mb = 50
use_https = False
socket_timeout = 30
EOD

## create crypt4gh key
if [ ! -f "/shared/crypt4gh" ]; then
    echo "downloading crypt4gh"
    latest_c4gh=$(curl --retry 100 -sL https://api.github.com/repos/neicnordic/crypt4gh/releases/latest | jq -r '.name')
    curl --retry 100 -s -L "https://github.com/neicnordic/crypt4gh/releases/download/$latest_c4gh/crypt4gh_linux_x86_64.tar.gz" | tar -xz -C /shared/ && chmod +x /shared/crypt4gh
fi

if [ ! -f "/shared/c4gh.sec.pem" ]; then
    echo "creating crypth4gh key"
    /shared/crypt4gh generate -n /shared/c4gh -p c4ghpass
fi

if [ ! -f "/shared/client.sec.pem" ]; then # client key for re-encryption
    echo "creating client crypth4gh key"
    /shared/crypt4gh generate -n /shared/client -p c4ghpass
fi

if [ ! -f "/shared/sync.sec.pem" ]; then
    echo "creating sync crypth4gh key"
    /shared/crypt4gh generate -n /shared/sync -p syncPass
fi

if [ ! -f "/shared/rotatekey.sec.pem" ]; then
    echo "creating rotatekey crypth4gh key"
    /shared/crypt4gh generate -n /shared/rotatekey -p rotatekeyPass
fi

# register the rotation key in the db
resp=$(psql -U postgres -h postgres -d sda -At -c "SELECT description FROM sda.encryption_keys;")
if ! echo "$resp" | grep -q 'this is the new key to rotate to'; then
    rotateKeyHash=$(cat /shared/rotatekey.pub.pem | awk 'NR==2' | base64 -d | xxd -p -c256)
    resp=$(psql -U postgres -h postgres -d sda -At -c "INSERT INTO sda.encryption_keys(key_hash, description) VALUES('$rotateKeyHash', 'this is the new key to rotate to');")
    if [ "$(echo "$resp" | tr -d '\n')" != "INSERT 0 1" ]; then
        echo "insert keyhash failed"
        exit 1
    fi
fi

if [ ! -f "/shared/keys/ssh" ]; then
    ssh-keygen -o -a 256 -t ed25519 -f /shared/keys/ssh -N ""
    pubKey="$(cat /shared/keys/ssh.pub)"
    cat >/shared/users.json <<EOD
[
    {
        "username": "dummy@example.com",
        "uid": 1,
        "passwordHash": "\$2b\$12\$1gyKIjBc9/cT0MYkXX24xe1LjEUjNwgL4rEk8fDoO.vDQZzWkqrn.",
        "gecos": "dummy user",
        "sshPublicKey": ["$pubKey"],
        "enabled": null
    }
]
EOD
fi

## download grpcurl
if [ ! -f "/shared/grpcurl" ]; then
    echo "downloading grpcurl"
    latest_grpculr=$(curl --retry 100 -sL https://api.github.com/repos/fullstorydev/grpcurl/releases/latest | jq -r '.name' | sed -e 's/v//')
    curl --retry 100 -s -L "https://github.com/fullstorydev/grpcurl/releases/download/v${latest_grpculr}/grpcurl_${latest_grpculr}_linux_x86_64.tar.gz" | tar -xz -C /shared/ && chmod +x /shared/grpcurl
fi
