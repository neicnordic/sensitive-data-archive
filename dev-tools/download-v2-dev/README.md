# Download API v2 - Local Development

Lightweight dev stack for building applications against the sda-download v2 API.

## Quick Start

```bash
# Build images first (only needed once, or after code changes)
make build-all

# Start all services
make dev-download-v2-up

# Get a token
TOKEN=$(curl -s http://localhost:8000/tokens | jq -r '.[0]')

# List datasets
curl -H "Authorization: Bearer $TOKEN" http://localhost:8085/datasets

# List files in dataset
curl -H "Authorization: Bearer $TOKEN" http://localhost:8085/datasets/EGAD00000000001/files
```

## Services

| Service  | Port  | Description                          |
|----------|-------|--------------------------------------|
| download | 8085  | Download API v2                      |
| mockauth | 8000  | Mock OIDC (JWKS, userinfo, /tokens)  |
| postgres | 15432 | PostgreSQL with SDA schema           |
| minio    | 19000 | S3 storage (console at 19001)        |
| reencrypt| 50051 | gRPC re-encryption for file downloads|

## Getting Tokens

```bash
# Get dev token (JSON array with one entry)
curl http://localhost:8000/tokens

# Token for integration_test@example.org (has access to all test datasets)
TOKEN=$(curl -s http://localhost:8000/tokens | jq -r '.[0]')
```

## API Endpoints

Full documentation: [sda/cmd/download/download.md](../../sda/cmd/download/download.md)

```bash
# Health check
curl http://localhost:8085/health/ready

# List datasets
curl -H "Authorization: Bearer $TOKEN" http://localhost:8085/datasets

# Dataset details
curl -H "Authorization: Bearer $TOKEN" http://localhost:8085/datasets/EGAD00000000001

# List files in dataset
curl -H "Authorization: Bearer $TOKEN" http://localhost:8085/datasets/EGAD00000000001/files

# File metadata
curl -H "Authorization: Bearer $TOKEN" http://localhost:8085/files/EGAF00000000001

# DRS object resolution
curl -H "Authorization: Bearer $TOKEN" http://localhost:8085/objects/EGAD00000000001/test-file.c4gh

# Get the c4gh public key from the running stack
docker cp $(docker ps -qf label=com.docker.compose.project=download-v2-dev -f label=com.docker.compose.service=reencrypt | head -1):/shared/c4gh.pub.pem /tmp/dev-c4gh.pub.pem
C4GH_KEY=$(base64 -w0 /tmp/dev-c4gh.pub.pem)

# Download a file (requires c4gh public key)
curl -H "Authorization: Bearer $TOKEN" \
     -H "X-C4GH-Public-Key: $C4GH_KEY" \
     http://localhost:8085/files/EGAF00000000001 -o file.c4gh

# Download content only (stable ETag, supports Range requests)
curl -H "Authorization: Bearer $TOKEN" \
     -H "X-C4GH-Public-Key: $C4GH_KEY" \
     http://localhost:8085/files/EGAF00000000001/content -o content.c4gh
```

## Test Data

The stack is pre-seeded with:

- **Dataset**: `EGAD00000000001` ("Test Dataset")
- **File**: `EGAF00000000001` (`test-file.c4gh`, 1000 bytes archived, 500 bytes decrypted)
- **User**: `integration_test@example.org` (file owner)

## Using with sda-auth

A JWT from `sda-auth` works with this download service as long as the public key
matches. Configure the download service with `JWT_PUBKEY_PATH` or `JWT_PUBKEY_URL`
pointing to sda-auth's public key.

## Interactive Demo

Run the sprint review demo script to see all endpoints in action:

```bash
./dev-tools/download-v2-dev/demo.sh        # all 7 steps
./dev-tools/download-v2-dev/demo.sh 6      # single step (e.g. file download)
```

## Limitations

- **Authorization is bypassed.** The config sets `jwt.allow-all-data: true` so any valid
  JWT can access all datasets. This simplifies getting started but means permission
  denied paths (ownership model, visa grants) are not exercised locally. When building
  auth flows against sda-auth, be aware that access control will behave differently in
  real deployments.

## Cleanup

```bash
make dev-download-v2-down
```
