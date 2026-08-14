#!/bin/sh
set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
. "$SCRIPT_DIR/helpers.sh"

URI=http://rabbitmq:15672

# Postgres requires a client certificate, so this is a simple way of detecting if TLS is enabled or not.
if [ -n "$PGSSLCERT" ]; then
    URI=https://rabbitmq:15671
fi

export DEBIAN_FRONTEND=noninteractive
echo 'Acquire::ForceIPv4 "true";' > /etc/apt/apt.conf.d/99force-ipv4

apt-get -o DPkg::Lock::Timeout=60 update > /dev/null
apt-get -o DPkg::Lock::Timeout=60 install -y curl jq openssh-client openssl postgresql-client xxd > /dev/null

python -m pip install --upgrade pip > /dev/null
python -m pip install aiohttp Authlib joserfc requests > /dev/null

echo "Waiting for RabbitMQ Management API to start..."
until curl -s -k -u guest:guest "$URI/api/vhosts" > /dev/null; do
  sleep 2
done

# Provision user profiles in DB and register matching service roles in MQ broker
for n in api auth download finalize inbox ingest mapper rotatekey sync verify; do
    echo "creating credentials for: $n"
    # Ensure the role actually exists before modifying it
    psql -U postgres -h postgres -d sda -c "CREATE ROLE $n;" || true
    psql -U postgres -h postgres -d sda -c "ALTER ROLE $n LOGIN PASSWORD '$n';" || true
    psql -U postgres -h postgres -d sda -c "GRANT base TO $n;" || true

    ## password and permissions for MQ
    body_data=$(jq -n -c --arg password "$n" --arg tags none '$ARGS.named')
    curl -fsS --connect-timeout 5 --max-time 20 -u guest:guest -X PUT -k "$URI/api/users/$n" -H "content-type:application/json" -d "${body_data}"
    curl -fsS --connect-timeout 5 --max-time 20 -u guest:guest -X PUT -k "$URI/api/permissions/sda/$n" -H "content-type:application/json" -d '{"configure":".*","write":".*","read":".*"}'
done

# Create signing keys
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

echo "creating credentials tokens"
# Parse key with jwk.import_key before feeding it into the encoder
TOKEN=$(python3 -c "
import time
from joserfc import jwk, jwt
header = {'alg': 'RS256', 'typ': 'JWT', 'kid': 'rsa1'}
payload = {
    'iss': 'http://mockauth:8000',
    'sub': 'test@dummy.org',
    'aud': 'XC56EL11xx',
    'iat': int(time.time()),
    'exp': int(time.time()) + 360000,
    'jti': 'dev-token-generation'
}
with open('/shared/keys/jwt.key', 'r') as f:
    raw_pem = f.read()

# Import the string raw key format into a joserfc key object
key = jwk.import_key(raw_pem)

# Generate token string payload
token_bytes = jwt.encode(header, payload, key)
print(token_bytes.decode('utf-8') if isinstance(token_bytes, bytes) else token_bytes)
")

echo "$TOKEN" > /shared/token

# Create the requested s3cfg config file
cat >/shared/s3cfg <<EOD
[default]
access_key=test@dummy.org
secret_key=test@dummy.org
access_token=${TOKEN}
check_ssl_certificate = False
check_ssl_hostname = False
encoding = UTF-8
encrypt = False
guess_mime_type = True
host_base = s3inbox:8000
host_bucket = s3inbox:8000
bucket_location = us-east-1
human_readable_sizes = true
multipart_chunk_size_mb = 50
use_https = False
socket_timeout = 30
EOD

# === Generate Trust Store parameters for Validation ===
cat > "/shared/trusted-issuers.json" <<'ISSUERS'
[
  {
    "iss": "http://mockauth:8000",
    "jku": "http://mockauth:8000/jwks"
  }
]
ISSUERS

## create crypt4gh key utilities
if [ ! -f "/shared/crypt4gh" ]; then
    echo "downloading crypt4gh"
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64) CRYPT4GH_ARCH="linux_x86_64" ;;
        aarch64) CRYPT4GH_ARCH="linux_arm64" ;;
        *)
            echo "Unknown architecture: $ARCH. Defaulting to linux_x86_64."
            CRYPT4GH_ARCH="linux_x86_64"
            ;;
    esac
    echo "Detected architecture: $ARCH, downloading crypt4gh for: $CRYPT4GH_ARCH"
    latest_c4gh=$(curl -4 --retry 5 --retry-delay 2 --connect-timeout 10 --max-time 30 -fsSL \
      https://api.github.com/repos/neicnordic/crypt4gh/releases/latest | jq -r '.name')
    curl -4 --retry 5 --retry-delay 2 --connect-timeout 10 --max-time 120 -fSL \
      "https://github.com/neicnordic/crypt4gh/releases/download/$latest_c4gh/crypt4gh_${CRYPT4GH_ARCH}.tar.gz" \
      | tar -xz -C /shared/ && chmod +x /shared/crypt4gh
fi

if [ ! -f "/shared/c4gh.sec.pem" ]; then
    echo "creating crypth4gh key"
    /shared/crypt4gh generate -n /shared/c4gh -p c4ghpass
fi

if [ ! -f "/shared/client.sec.pem" ]; then
    echo "creating client crypth4gh key"
    /shared/crypt4gh generate -n /shared/client -p c4ghpass
fi

if [ ! -f "/shared/rotatekey.sec.pem" ]; then
    echo "creating rotatekey crypth4gh key"
    /shared/crypt4gh generate -n /shared/rotatekey -p rotatekeyPass
fi

# Generate 4 Extra Crypt4GH keys
for i in 1 2 3 4; do
    echo "creating extra crypth4gh key $i"
    /shared/crypt4gh generate -n "/shared/extra_key_$i" -p "pass$i"
done

# Generate Deprecated Crypt4GH key pair
if [ ! -f "/shared/deprecated_key.sec.pem" ]; then
    /shared/crypt4gh generate -n "/shared/deprecated_key" -p "deprecatedpass"
fi

# register the crypt4gh keys in the db (idempotent)
for keyfile in c4gh rotatekey deprecated_key; do
    keyHash=$(compute_key_hash "/shared/${keyfile}.pub.pem")
    resp=$(psql -U postgres -h postgres -d sda -At -c "INSERT INTO sda.encryption_keys(key_hash, description) VALUES('$keyHash', 'this is the $keyfile key') ON CONFLICT (key_hash) DO UPDATE SET description = EXCLUDED.description;")
    case "$(echo "$resp" | tr -d '\n')" in
        "INSERT 0 1"|"INSERT 0 0") : ;;
        *)
            echo "insert/upsert keyhash failed for $keyfile"
            exit 1
            ;;
    esac
    # Handle deprecated_key timestamp setting
    if [ "$keyfile" = "deprecated_key" ]; then
        dep_resp=$(psql -U postgres -h postgres -d sda -At -c "UPDATE sda.encryption_keys SET deprecated_at = NOW() - INTERVAL '1 day' WHERE key_hash = '$keyHash';")
        case "$(echo "$dep_resp" | tr -d '\n')" in
            "UPDATE 1"|"UPDATE 0") : ;;
            *)
                echo "setting deprecated_at failed for $keyfile: $dep_resp"
                exit 1
                ;;
        esac
    fi
done

if [ ! -f "/shared/keys/ssh" ]; then
    ssh-keygen -o -a 256 -t ed25519 -f /shared/keys/ssh -N ""
    pubKey="$(cat /shared/keys/ssh.pub)"
    cat >/shared/users.json <<EOD
[
    {
        "username": "test@dummy.org",
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
    latest_grpcurl=$(curl -4 --retry 5 --retry-delay 2 --connect-timeout 10 --max-time 30 -fsSL \
      https://api.github.com/repos/fullstorydev/grpcurl/releases/latest | jq -r '.name' | sed -e 's/v//')
    curl -4 --retry 5 --retry-delay 2 --connect-timeout 10 --max-time 120 -fSL \
      "https://github.com/fullstorydev/grpcurl/releases/download/v${latest_grpcurl}/grpcurl_${latest_grpcurl}_linux_x86_64.tar.gz" \
      | tar -xz -C /shared/ && chmod +x /shared/grpcurl
fi

echo "Credentials script setup completed successfully!"