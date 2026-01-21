# Download

The Download service provides secure file download functionality for archived data.
Users are authenticated with a JWT token, and files are re-encrypted with the user's
public key before download.

## Service Description

This service implements the Data Out API for the NeIC Sensitive Data Archive. It allows
authenticated users to browse datasets and download files that have been archived and
verified.

### Authentication

All endpoints except health checks require JWT authentication. The token must be:
- Provided in the `Authorization: Bearer <token>` header
- Signed with a key matching the configured JWKS (via `jwt.pubkey-path` or `jwt.pubkey-url`)

### Permission Model

The service supports two permission modes (configured via `jwt.allow-all-data`):

| Mode                    | Description                                              |
|-------------------------|----------------------------------------------------------|
| `allow-all-data: true`  | All authenticated users can access all datasets (testing)|
| `allow-all-data: false` | Users can only access datasets they submitted            |

In production, the data ownership model is used: users have access to datasets
containing files where they are the `submission_user`.

## Endpoints

### Health Endpoints

#### `/health/ready`
- accepts `GET` requests
- Returns readiness status with dependency checks

  Example:

  ```bash
  curl https://HOSTNAME/health/ready
  {"status":"ok","services":{"database":"ok","storage":"ok","grpc":"ok","oidc":"ok"}}
  ```

#### `/health/live`
- accepts `GET` requests
- Returns simple liveness status

  Example:

  ```bash
  curl https://HOSTNAME/health/live
  {"status":"ok"}
  ```

### Info Endpoints

#### `/info/datasets`
- accepts `GET` requests
- Returns all datasets the authenticated user has access to

- Error codes
  - `200` Query executed successfully
  - `401` Invalid or missing token
  - `500` Internal error due to DB failures

  Example:

  ```bash
  curl -H "Authorization: Bearer $token" https://HOSTNAME/info/datasets
  [{"id":"EGAD74900000101","title":"Example Dataset"}]
  ```

#### `/info/dataset`
- accepts `GET` requests with query parameter `dataset`
- Returns metadata for a specific dataset

- Error codes
  - `200` Query executed successfully
  - `400` Missing dataset parameter
  - `401` Invalid or missing token
  - `403` Access denied to dataset
  - `404` Dataset not found
  - `500` Internal error due to DB failures

  Example:

  ```bash
  curl -H "Authorization: Bearer $token" "https://HOSTNAME/info/dataset?dataset=EGAD74900000101"
  {"id":"EGAD74900000101","title":"Example Dataset","fileCount":2,"totalSize":15242998}
  ```

#### `/info/dataset/files`
- accepts `GET` requests with query parameter `dataset`
- Returns list of files in the specified dataset

- Error codes
  - `200` Query executed successfully
  - `400` Missing dataset parameter
  - `401` Invalid or missing token
  - `403` Access denied to dataset
  - `500` Internal error due to DB failures

  Example:

  ```bash
  curl -H "Authorization: Bearer $token" "https://HOSTNAME/info/dataset/files?dataset=EGAD74900000101"
  [{"id":"EGAF74900000001","path":"data/file1.c4gh","size":1024000,"checksum":"abc123...","checksumType":"SHA256"}]
  ```

### File Download Endpoints

File downloads require a `public_key` header containing the user's Crypt4GH public key
(base64-encoded). The archived file is re-encrypted with this key before streaming to
the client.

#### `/file/:fileId`
- accepts `GET` requests with file stable ID in path
- Downloads a file by its stable ID (e.g., `EGAF74900000001`)
- Supports HTTP Range requests for partial downloads

- Request Headers
  - `Authorization: Bearer <token>` (required)
  - `public_key: <base64-encoded-c4gh-public-key>` (required)
  - `Range: bytes=START-END` (optional, for partial downloads)

- Response Headers
  - `Content-Type: application/octet-stream`
  - `Content-Disposition: attachment; filename="<fileId>.c4gh"`
  - `Accept-Ranges: bytes`
  - `X-File-Id: <fileId>`
  - `X-Decrypted-Size: <size>`
  - `X-Decrypted-Checksum: <checksum>` (if available)
  - `X-Decrypted-Checksum-Type: <type>` (if available)

- Error codes
  - `200` Download successful
  - `206` Partial content (Range request)
  - `400` Missing fileId or public_key header
  - `401` Invalid or missing token
  - `403` Access denied to file
  - `404` File not found
  - `500` Internal error (storage, reencrypt, or streaming failures)

  Example:

  ```bash
  curl -H "Authorization: Bearer $token" \
       -H "public_key: $(base64 -w0 /path/to/c4gh.pub.pem)" \
       https://HOSTNAME/file/EGAF74900000001 \
       -o downloaded_file.c4gh
  ```

  Example with Range header:

  ```bash
  curl -H "Authorization: Bearer $token" \
       -H "public_key: $(base64 -w0 /path/to/c4gh.pub.pem)" \
       -H "Range: bytes=0-1023" \
       https://HOSTNAME/file/EGAF74900000001
  ```

#### `/file`
- accepts `GET` requests with query parameters
- Downloads a file by dataset and either fileId or filePath
- Supports HTTP Range requests for partial downloads

- Query Parameters
  - `dataset` (required): Dataset stable ID
  - `fileId` (optional): File stable ID within the dataset
  - `filePath` (optional): File path within the dataset
  - Note: Either `fileId` or `filePath` must be provided, but not both

- Request Headers: Same as `/file/:fileId`
- Response Headers: Same as `/file/:fileId`

- Error codes
  - `200` Download successful
  - `206` Partial content (Range request)
  - `400` Missing required parameters or invalid combination
  - `401` Invalid or missing token
  - `403` Access denied to file
  - `404` File not found
  - `500` Internal error

  Example by fileId:

  ```bash
  curl -H "Authorization: Bearer $token" \
       -H "public_key: $(base64 -w0 /path/to/c4gh.pub.pem)" \
       "https://HOSTNAME/file?dataset=EGAD74900000101&fileId=EGAF74900000001" \
       -o downloaded_file.c4gh
  ```

  Example by filePath:

  ```bash
  curl -H "Authorization: Bearer $token" \
       -H "public_key: $(base64 -w0 /path/to/c4gh.pub.pem)" \
       "https://HOSTNAME/file?dataset=EGAD74900000101&filePath=data/sample.c4gh" \
       -o downloaded_file.c4gh
  ```

## Configuration

The service is configured via YAML config file or environment variables.

### API Server Settings

| Variable        | Config Key   | Description                    | Default     |
|-----------------|--------------|--------------------------------|-------------|
| `API_HOST`      | `api.host`   | Host to bind to                | `0.0.0.0`   |
| `API_PORT`      | `api.port`   | Port to listen on              | `8080`      |

### gRPC Health Check Settings

| Variable        | Config Key    | Description                   | Default     |
|-----------------|---------------|-------------------------------|-------------|
| `HEALTH_PORT`   | `health.port` | gRPC health check port        | `8081`      |

### Database Settings

| Variable        | Config Key    | Description                   |
|-----------------|---------------|-------------------------------|
| `DB_HOST`       | `db.host`     | PostgreSQL hostname           |
| `DB_PORT`       | `db.port`     | PostgreSQL port               |
| `DB_USER`       | `db.user`     | Database username             |
| `DB_PASSWORD`   | `db.password` | Database password             |
| `DB_DATABASE`   | `db.database` | Database name                 |
| `DB_SSLMODE`    | `db.sslmode`  | SSL mode (disable/require/etc)|

### JWT/Authentication Settings

| Variable              | Config Key          | Description                           |
|-----------------------|---------------------|---------------------------------------|
| `JWT_PUBKEY_PATH`     | `jwt.pubkey-path`   | Path to directory with public keys    |
| `JWT_PUBKEY_URL`      | `jwt.pubkey-url`    | JWKS URL for key fetching             |
| `JWT_ALLOW_ALL_DATA`  | `jwt.allow-all-data`| Allow all authenticated users access  |

At least one of `jwt.pubkey-path` or `jwt.pubkey-url` must be configured.

### gRPC Reencrypt Service Settings

| Variable        | Config Key    | Description                   |
|-----------------|---------------|-------------------------------|
| `GRPC_HOST`     | `grpc.host`   | Reencrypt service hostname    |
| `GRPC_PORT`     | `grpc.port`   | Reencrypt service port        |
| `GRPC_TIMEOUT`  | `grpc.timeout`| Request timeout in seconds    |

### Storage Settings

Storage is configured using the `storage/v2` package. See [storage/v2 README](../../internal/storage/v2/README.md) for details.

Example S3 configuration:

```yaml
storage:
  backend: "archive"
  archive:
    s3:
      - endpoint: "s3.example.com"
        access_key: "access"
        secret_key: "secret"
        region: "us-east-1"
        bucket_prefix: "archive"
```

### Session Settings

| Variable              | Config Key          | Description                       |
|-----------------------|---------------------|-----------------------------------|
| `SESSION_EXPIRATION`  | `session.expiration`| Session expiration in seconds     |
| `SESSION_SECURE`      | `session.secure`    | Use secure cookies                |
| `SESSION_HTTP_ONLY`   | `session.http-only` | HTTP-only cookies                 |
| `SESSION_NAME`        | `session.name`      | Session cookie name               |

### Cache Settings

Database query results are cached in-memory to reduce database roundtrips, which is
particularly beneficial for streaming use cases where the same file metadata may be
requested multiple times (e.g., HTTP Range requests).

| Variable               | Config Key            | Description                          | Default |
|------------------------|-----------------------|--------------------------------------|---------|
| `CACHE_ENABLED`        | `cache.enabled`       | Enable database query caching        | `true`  |
| `CACHE_FILE_TTL`       | `cache.file-ttl`      | TTL for file queries (seconds)       | `300`   |
| `CACHE_PERMISSION_TTL` | `cache.permission-ttl`| TTL for permission checks (seconds)  | `120`   |
| `CACHE_DATASET_TTL`    | `cache.dataset-ttl`   | TTL for dataset queries (seconds)    | `300`   |

The cache uses [ristretto](https://github.com/dgraph-io/ristretto), a high-performance
concurrent cache. Cached queries include:

- File lookups by ID and path
- Permission checks (user-scoped via dataset IDs)
- Dataset listings and metadata
- Dataset file listings

Cache keys for permission checks are scoped by the user's accessible datasets, ensuring
that users with different permissions get appropriately cached results.

## Testing

### Integration Tests

Run the standalone Go-based integration tests:

```bash
docker compose -f .github/integration/sda-download-integration.yml run integration_test
```

Or run as part of the full SDA pipeline:

```bash
make integrationtest-sda-s3-run
```

### Local Development

See [TESTING.md](TESTING.md) for detailed local testing instructions.
