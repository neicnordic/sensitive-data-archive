# Local Testing Guide for sda/cmd/download

This guide explains how to test the new download service locally against the integration test infrastructure.

## Prerequisites

- Docker and Docker Compose v2+
- Go 1.20+
- `make` utility
- `curl` and `jq` for testing

## Quick Start

### Step 1: Build and Start Infrastructure

From the repository root:

```bash
# Build all containers
make build-all

# Start the S3 integration environment
make sda-s3-up
```

Wait for all services to be healthy (about 30-60 seconds). You can check with:

```bash
docker ps
```

Services available:

| Service    | Port                    | Description              |
| ---------- | ----------------------- | ------------------------ |
| PostgreSQL | 15432                   | Database                 |
| MinIO (S3) | 19000 (API), 19001 (UI) | Object storage           |
| RabbitMQ   | 5672, 15672             | Message broker           |
| ls-aai-mock| 8800                    | OIDC/JWKS provider       |
| auth-aai   | 8801                    | Auth service with login  |
| reencrypt  | 50051                   | gRPC reencryption        |
| api        | 8090                    | SDA API service          |

### Step 2: Run Integration Tests to Populate Test Data

The integration tests upload files, ingest them, and create datasets:

```bash
# Run the full integration test suite
make integrationtest-sda-s3-run
```

After tests complete, you'll have:

- Files with stable IDs like `EGAF74900000001`, `EGAF74900000002`
- Dataset `EGAD74900000101` mapped to these files
- User `test_dummy.org` who submitted the files

### Step 3: Start the Download Service Locally

```bash
cd sda
CONFIGFILE=cmd/download/dev_config.yaml go run cmd/download/main.go
```

The service starts on:

- HTTP API: http://localhost:8080
- gRPC Health: localhost:8081

### Step 4: Get an Access Token

```bash
# Option 1: Use pre-generated token from integration tests
TOKEN=$(docker exec integration_test cat /shared/token 2>/dev/null)

# Option 2: Get token from ls-aai-mock
TOKEN=$(curl -s http://localhost:8800/oidc/token | jq -r '.access_token')

# Option 3: Log in via browser at http://localhost:8801 and copy the token
```

**Note:** The download service uses JWKS-based JWT validation. The token must be signed
with a key that matches the configured `jwt.pubkey-path` or `jwt.pubkey-url`.

### Permission Model

The service supports two permission modes (configured via `jwt.allow-all-data`):

| Mode                        | Description                                        |
| --------------------------- | -------------------------------------------------- |
| `allow-all-data: true`      | All authenticated users can access all datasets    |
| `allow-all-data: false`     | Users can only access datasets they submitted      |

The default integration test config uses `allow-all-data: true` for easier testing.

### Step 5: Test the API

#### Health Check

```bash
curl http://localhost:8080/health/live
# {"status":"ok"}

curl http://localhost:8080/health/ready
# {"status":"ok","services":{"database":"ok",...}}
```

#### List Datasets (requires authentication)

```bash
curl -H "Authorization: Bearer $TOKEN" \
     http://localhost:8080/info/datasets
```

#### Get Dataset Info

```bash
curl -H "Authorization: Bearer $TOKEN" \
     "http://localhost:8080/info/dataset?dataset=EGAD74900000101"
```

#### List Files in Dataset

```bash
curl -H "Authorization: Bearer $TOKEN" \
     "http://localhost:8080/info/dataset/files?dataset=EGAD74900000101"
```

#### Download a File

```bash
# Generate a crypt4gh key pair if needed
# crypt4gh generate --name mykey

# Full file download
curl -H "Authorization: Bearer $TOKEN" \
     -H "public_key: $(base64 -w0 mykey.pub.pem)" \
     http://localhost:8080/file/EGAF74900000001 \
     -o downloaded_file.c4gh

# Partial download (Range header)
curl -H "Authorization: Bearer $TOKEN" \
     -H "public_key: $(base64 -w0 mykey.pub.pem)" \
     -H "Range: bytes=0-1023" \
     http://localhost:8080/file/EGAF74900000001
```

## Database Queries for Debugging

Connect to the database:

```bash
docker exec -it postgres psql -U postgres -d sda
```

Useful queries:

```sql
-- List all files with stable IDs
SELECT id, stable_id, submission_file_path, submission_user
FROM sda.files
WHERE stable_id IS NOT NULL;

-- List all datasets
SELECT id, stable_id, title
FROM sda.datasets;

-- Files in a dataset
SELECT f.stable_id, f.submission_file_path, f.submission_user
FROM sda.files f
JOIN sda.file_dataset fd ON f.id = fd.file_id
JOIN sda.datasets d ON fd.dataset_id = d.id
WHERE d.stable_id = 'EGAD74900000101';

-- Datasets owned by a user (data ownership model)
SELECT DISTINCT d.stable_id
FROM sda.datasets d
JOIN sda.file_dataset fd ON d.id = fd.dataset_id
JOIN sda.files f ON fd.file_id = f.id
WHERE f.submission_user = 'test_dummy.org';
```

## MinIO Console

Access MinIO at http://localhost:19001 with:

- Username: `access`
- Password: `secretKey`

## Cleanup

```bash
# Stop and remove all containers
make sda-s3-down
```

## Troubleshooting

### "Config File Not Found"

This is a viper warning, not an error. The service loads config from `CONFIGFILE` environment variable.

### Database Connection Issues

Ensure PostgreSQL is running:

```bash
docker logs postgres
```

### Storage Reader Errors

Check that storage configuration in `dev_config.yaml` matches the running Docker environment.

### JWT/Token Issues

1. Ensure ls-aai-mock is running: `docker logs ls-aai-mock`
2. Check JWKS endpoint: `curl http://localhost:8800/oidc/jwk`
3. Verify token is valid: `curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/info/datasets`

### No Datasets Returned

If `/info/datasets` returns empty:

1. Check integration tests completed successfully
2. Verify files have stable IDs: `SELECT stable_id FROM sda.files WHERE stable_id IS NOT NULL;`
3. If using `allow-all-data: false`, ensure you're authenticated as the user who submitted the files
