# Download

The download service provides the Data Out API for the NeIC Sensitive Data Archive.
It replaces both the standalone [sda-download](https://github.com/neicnordic/sda-download)
Go service and the Java-based [sda-doa](https://github.com/neicnordic/sda-doa), unifying
data retrieval into a single service within the `sda` binary.

Key improvements over the previous implementations:

- **GA4GH Passport/Visa support** with trusted issuer allowlists and JWKS validation
- **Three permission models**: ownership-only, visa-only, or combined
- **Split file endpoints** — separate header and content downloads for efficient Crypt4GH operations
- **Keyset pagination** with HMAC-signed page tokens
- **Multi-layer caching** — database queries, sessions, tokens, JWK sets, and visa validation results
- **Audit logging** for compliance tracking
- **Production safety guards** that prevent dangerous configurations in production

## Service Description

The service is a standalone HTTP server built with Gin. It connects to:

- **PostgreSQL** — file/dataset metadata and permissions
- **S3/POSIX storage** — archived file data
- **gRPC reencrypt service** — Crypt4GH re-encryption for downloads
- **OIDC provider** (optional) — userinfo endpoint for opaque tokens and GA4GH passports

On startup the service validates configuration, initializes caches, and runs
production safety guards when `app.environment` is set to `production`.

## Authentication

All endpoints except `/health/*` and `/service-info` require authentication.
Tokens are extracted from the `Authorization: Bearer <token>` header or the
`X-Amz-Security-Token` header.

The service uses structure-based detection to classify tokens:

### JWT Tokens

Tokens with three dot-separated base64url segments (valid JSON header and payload)
are treated as JWTs. They are validated locally against public keys loaded from
`jwt.pubkey-path` or fetched from `jwt.pubkey-url`. If `oidc.issuer` is configured,
the JWT `iss` claim must match. JWT validation failures are final — there is no
userinfo fallback.

### Opaque Tokens

When `auth.allow-opaque` is enabled (default: `true`), tokens that do not look like
JWTs are sent to the OIDC userinfo endpoint. The `sub` claim from the userinfo
response is used as the user identity. The issuer is taken from `oidc.issuer`.

### Session Caching

Authenticated sessions are cached in-memory keyed by `sha256(token)`. The cache TTL
is bounded by `min(token.exp, min(visa.exp), configured TTL)`. A session cookie
(configurable via `session.name`, default `sda_session`) provides fast lookups for
repeat requests. The legacy cookie name `sda_session_key` is also checked for
backwards compatibility.

## Permission Model

The permission model is configured via `permission.model` (default: `combined`).

| Model       | Description                                                         |
|-------------|---------------------------------------------------------------------|
| `ownership` | Users access datasets containing files where they are the `submission_user` |
| `visa`      | Access determined solely by GA4GH ControlledAccessGrants visas      |
| `combined`  | Union of ownership and visa datasets. Visa failures are non-fatal — ownership results are still served |

When `jwt.allow-all-data` is `true`, all authenticated users can access all
datasets regardless of the permission model. **This flag is blocked by production
safety guards.**

## GA4GH Visa Support

When `visa.enabled` is `true`, the service validates
[GA4GH Passport v1](https://github.com/ga4gh/data-security/blob/master/AAI/AAIConnectProfile.md)
visas to determine dataset access.

### Visa Validation Flow

1. After authentication, the userinfo endpoint is called to retrieve the `ga4gh_passport_v1` claim
2. Each visa JWT in the passport is validated:
   - The `(iss, jku)` pair must appear in the trusted issuers allowlist
   - The JWKS at the `jku` URL is fetched (cached) and the visa signature is verified
   - The visa must not be expired
3. Only `ControlledAccessGrants` type visas are processed:
   - `by` must be non-empty
   - `value` and `source` must be ≤ 255 characters
   - `conditions` must be empty
   - `asserted` must not be in the future (when `visa.validate-asserted` is `true`)
4. The `value` field determines dataset access. Unknown visa types are silently ignored per the GA4GH spec.

### Trusted Issuers

A JSON file at `visa.trusted-issuers-path` defines the allowed `(iss, jku)` pairs:

```json
[
  {"iss": "https://login.example.org", "jku": "https://login.example.org/jwks"}
]
```

Only visas from issuers in this allowlist are accepted. JKU URLs must use HTTPS
unless `visa.allow-insecure-jku` is enabled (testing only).

### Dataset ID Matching

Configured via `visa.dataset-id-mode`:

| Mode     | Description                                              |
|----------|----------------------------------------------------------|
| `raw`    | The visa `value` is used as-is as the dataset identifier |
| `suffix` | The last segment of the URL or URN path is extracted     |

### Identity Binding

Configured via `visa.identity.mode`:

| Mode              | Description                                                           |
|-------------------|-----------------------------------------------------------------------|
| `broker-bound`    | Default. Visa identity is not checked against the authenticated user  |
| `strict-sub`      | Visa `sub` must match the authenticated user's `sub`                  |
| `strict-iss-sub`  | Visa `iss`+`sub` must match the authenticated user's `iss`+`sub`      |

### Safety Limits

| Config                            | Default | Description                             |
|-----------------------------------|---------|-----------------------------------------|
| `visa.limits.max-visas`           | 200     | Maximum visas to process per passport   |
| `visa.limits.max-visa-size`       | 16384   | Maximum size per visa JWT (bytes)       |
| `visa.limits.max-jwks-per-request`| 10      | Maximum distinct JWKS fetches per request |

## Endpoints

### Health Endpoints

#### `GET /health/ready`

Returns readiness status with dependency checks for database, storage, gRPC, and OIDC.

Example:

```bash
curl https://HOSTNAME/health/ready
{"status":"ok","services":{"database":"ok","storage":"ok","grpc":"ok","oidc":"ok"}}
```

#### `GET /health/live`

Returns simple liveness status.

Example:

```bash
curl https://HOSTNAME/health/live
{"status":"ok"}
```

### Service Info

#### `GET /service-info`

Returns service metadata following the
[GA4GH service-info specification](https://github.com/ga4gh-discovery/ga4gh-service-info).
No authentication required.

Example:

```bash
curl https://HOSTNAME/service-info
```

### Dataset Endpoints

All dataset endpoints require authentication.

#### `GET /datasets`

Returns a paginated list of datasets accessible to the authenticated user.

- Query Parameters
  - `page_size` (optional): Number of results per page
  - `page_token` (optional): Opaque token for the next page

- Response: JSON array of dataset objects
- Error codes
  - `200` Success
  - `401` Invalid or missing token

Example:

```bash
curl -H "Authorization: Bearer $token" https://HOSTNAME/datasets
```

#### `GET /datasets/:datasetId`

Returns metadata for a specific dataset.

- Error codes
  - `200` Success
  - `401` Invalid or missing token
  - `403` Access denied or dataset does not exist

Example:

```bash
curl -H "Authorization: Bearer $token" https://HOSTNAME/datasets/EGAD00000000001
```

#### `GET /datasets/:datasetId/files`

Returns a paginated list of files in the specified dataset.

- Query Parameters
  - `page_size` (optional): Number of results per page
  - `page_token` (optional): Opaque token for the next page

- Error codes
  - `200` Success
  - `401` Invalid or missing token
  - `403` Access denied or dataset does not exist

Example:

```bash
curl -H "Authorization: Bearer $token" https://HOSTNAME/datasets/EGAD00000000001/files
```

### File Endpoints

All file endpoints require authentication. Download endpoints also require a Crypt4GH
public key in the `X-C4GH-Public-Key` header (base64-encoded).

Files are served through three tiers:

| Tier        | Endpoint                     | Content                         | Range Support |
|-------------|------------------------------|---------------------------------|---------------|
| Combined    | `GET /files/:fileId`         | Re-encrypted header + data      | Yes           |
| Header only | `GET /files/:fileId/header`  | Re-encrypted Crypt4GH header    | No            |
| Content only| `GET /files/:fileId/content` | Encrypted data segments         | Yes           |

All three tiers support `HEAD` requests for metadata without a response body.

#### `HEAD /files/:fileId`

Returns file metadata headers without downloading the file.

- Response Headers
  - `Content-Length`: total size (header + content)
  - `Accept-Ranges: bytes`
  - `ETag`: entity tag for caching

#### `GET /files/:fileId`

Downloads the complete re-encrypted file (header + data segments).

- Request Headers
  - `Authorization: Bearer <token>` (required)
  - `X-C4GH-Public-Key: <base64-encoded-key>` (required)
  - `Range: bytes=START-END` (optional)

- Response Headers
  - `Content-Type: application/octet-stream`
  - `Content-Disposition: attachment; filename="<fileId>.c4gh"`
  - `Accept-Ranges: bytes`

- Error codes
  - `200` Download successful
  - `206` Partial content (Range request)
  - `400` Missing public key header
  - `401` Invalid or missing token
  - `403` Access denied or file does not exist
  - `500` Internal error (storage, reencrypt, or streaming failure)

Example:

```bash
curl -H "Authorization: Bearer $token" \
     -H "X-C4GH-Public-Key: $(base64 -w0 /path/to/c4gh.pub.pem)" \
     https://HOSTNAME/files/EGAF00000000001 \
     -o downloaded_file.c4gh
```

#### `HEAD /files/:fileId/header` and `GET /files/:fileId/header`

Returns only the Crypt4GH header re-encrypted to the recipient's public key.
Useful when the client needs to process the header separately. Does not support
Range requests.

#### `HEAD /files/:fileId/content` and `GET /files/:fileId/content`

Returns only the encrypted data segments (without the Crypt4GH header).
Range byte offsets refer to the data segments only. The ETag is stable for a
given file and independent of the recipient key.

Example with Range:

```bash
curl -H "Authorization: Bearer $token" \
     -H "X-C4GH-Public-Key: $(base64 -w0 /path/to/c4gh.pub.pem)" \
     -H "Range: bytes=0-65535" \
     https://HOSTNAME/files/EGAF00000000001/content
```

### DRS Object Endpoint

#### `GET /objects/{datasetId}/{filePath}`

Returns a minimal [GA4GH DRS 1.5](https://ga4gh.github.io/data-repository-service-schemas/preview/release/drs-1.5.0/docs/)
`DrsObject` with a pre-resolved `access_url` pointing to the file content endpoint.
This enables DRS-aware clients (e.g. htsget-rs) to resolve a dataset + file path
to a download URL without knowing the internal file ID.

The path is composite: everything before the first `/` is the dataset ID, everything
after is the file path within the dataset.

- Error codes
  - `200` DRS object returned
  - `400` Malformed path (missing dataset or file component)
  - `401` Invalid or missing token
  - `403` Access denied or file does not exist

Example:

```bash
curl -H "Authorization: Bearer $token" \
     https://HOSTNAME/objects/EGAD00000000001/samples/sample1.bam.c4gh
```

Response:

```json
{
  "id": "EGAF00000000001",
  "self_uri": "drs://HOSTNAME/EGAF00000000001",
  "size": 1048576,
  "created_time": "2026-01-15T10:30:00Z",
  "checksums": [
    {"checksum": "a1b2c3d4...", "type": "sha-256"}
  ],
  "access_methods": [
    {
      "type": "https",
      "access_url": {
        "url": "https://HOSTNAME/files/EGAF00000000001/content"
      }
    }
  ]
}
```

The `size` and `checksums` describe the encrypted blob served by `access_url`,
per the DRS 1.5 specification.

### Error Format

All error responses use [RFC 9457 Problem Details](https://www.rfc-editor.org/rfc/rfc9457):

```json
{
  "type": "about:blank",
  "title": "Forbidden",
  "status": 403,
  "detail": "access denied"
}
```

Resource-by-ID endpoints (`/datasets/:datasetId`, `/files/:fileId`) return `403`
for both "access denied" and "does not exist" to prevent existence leakage.

## Configuration

The service is configured via YAML config file or environment variables.
Environment variables use uppercase with underscores replacing dots
(e.g., `api.host` → `API_HOST`).

Example:

```yaml
api:
  host: "0.0.0.0"
  port: 8080
db:
  host: "postgres"
  port: 5432
```

### API Server

| Variable         | Config Key       | Description                    | Default   |
|------------------|------------------|--------------------------------|-----------|
| `API_HOST`       | `api.host`       | Host address to bind to        | `0.0.0.0` |
| `API_PORT`       | `api.port`       | Port to listen on              | `8080`    |
| `API_SERVER_CERT`| `api.server-cert`| Path to TLS certificate        |           |
| `API_SERVER_KEY` | `api.server-key` | Path to TLS private key        |           |

### Service Info

| Variable            | Config Key        | Description                                      | Default        |
|---------------------|-------------------|--------------------------------------------------|----------------|
| `SERVICE_ID`        | `service.id`      | GA4GH service-info ID (reverse domain notation)  | `neicnordic.sda.download` |
| `SERVICE_ORG_NAME`  | `service.org-name`| Organization name for service-info               | *required*     |
| `SERVICE_ORG_URL`   | `service.org-url` | Organization URL for service-info                | *required*     |

### Database

| Variable      | Config Key    | Description                                         | Default     |
|---------------|---------------|-----------------------------------------------------|-------------|
| `DB_HOST`     | `db.host`     | PostgreSQL hostname                                 | `localhost` |
| `DB_PORT`     | `db.port`     | PostgreSQL port                                     | `5432`      |
| `DB_USER`     | `db.user`     | Database username                                   | *required*  |
| `DB_PASSWORD` | `db.password` | Database password                                   | *required*  |
| `DB_DATABASE` | `db.database` | Database name                                       | `sda`       |
| `DB_SSLMODE`  | `db.sslmode`  | SSL mode (disable, allow, prefer, require, verify-ca, verify-full) | `prefer` |
| `DB_CACERT`   | `db.cacert`   | Path to CA certificate for database TLS             |             |
| `DB_CLIENTCERT`| `db.clientcert`| Path to client certificate for database mTLS       |             |
| `DB_CLIENTKEY` | `db.clientkey` | Path to client key for database mTLS               |             |

### JWT / Authentication

| Variable             | Config Key           | Description                                      | Default |
|----------------------|----------------------|--------------------------------------------------|---------|
| `JWT_PUBKEY_PATH`    | `jwt.pubkey-path`    | Path to directory with PEM public keys           |         |
| `JWT_PUBKEY_URL`     | `jwt.pubkey-url`     | JWKS URL for key fetching                        |         |
| `JWT_ALLOW_ALL_DATA` | `jwt.allow-all-data` | Allow all authenticated users access (testing only) | `false` |
| `AUTH_ALLOW_OPAQUE`  | `auth.allow-opaque`  | Allow opaque tokens via userinfo                 | `true`  |

At least one of `jwt.pubkey-path` or `jwt.pubkey-url` must be configured.

### OIDC

| Variable        | Config Key      | Description                       | Default |
|-----------------|-----------------|-----------------------------------|---------|
| `OIDC_ISSUER`   | `oidc.issuer`   | Expected JWT issuer (optional)    |         |
| `OIDC_AUDIENCE` | `oidc.audience` | Expected JWT audience (optional)  |         |

### Permission

| Variable           | Config Key         | Description                              | Default    |
|--------------------|--------------------|------------------------------------------|------------|
| `PERMISSION_MODEL` | `permission.model` | Permission model: ownership, visa, or combined | `combined` |

### GA4GH Visa

| Variable                          | Config Key                       | Description                                    | Default        |
|-----------------------------------|----------------------------------|------------------------------------------------|----------------|
| `VISA_ENABLED`                    | `visa.enabled`                   | Enable GA4GH visa support                      | `false`        |
| `VISA_SOURCE`                     | `visa.source`                    | Visa source: `userinfo` or `token`             | `userinfo`     |
| `VISA_USERINFO_URL`               | `visa.userinfo-url`              | Userinfo endpoint (discovered from OIDC if not set) |            |
| `VISA_TRUSTED_ISSUERS_PATH`       | `visa.trusted-issuers-path`      | Path to JSON trusted issuers file              |                |
| `VISA_ALLOW_INSECURE_JKU`         | `visa.allow-insecure-jku`        | Allow HTTP JKU URLs (testing only)             | `false`        |
| `VISA_DATASET_ID_MODE`            | `visa.dataset-id-mode`           | Dataset ID mode: `raw` or `suffix`             | `raw`          |
| `VISA_IDENTITY_MODE`              | `visa.identity.mode`             | Identity binding: `broker-bound`, `strict-sub`, `strict-iss-sub` | `broker-bound` |
| `VISA_VALIDATE_ASSERTED`          | `visa.validate-asserted`         | Reject visas with future asserted timestamps   | `true`         |
| `VISA_LIMITS_MAX_VISAS`           | `visa.limits.max-visas`          | Max visas per passport                         | `200`          |
| `VISA_LIMITS_MAX_JWKS_PER_REQUEST`| `visa.limits.max-jwks-per-request`| Max distinct JWKS fetches per request          | `10`           |
| `VISA_LIMITS_MAX_VISA_SIZE`       | `visa.limits.max-visa-size`      | Max visa JWT size (bytes)                      | `16384`        |
| `VISA_CACHE_TOKEN_TTL`            | `visa.cache.token-ttl`           | Token cache TTL (seconds)                      | `3600`         |
| `VISA_CACHE_JWK_TTL`              | `visa.cache.jwk-ttl`             | JWK cache TTL (seconds)                        | `300`          |
| `VISA_CACHE_VALIDATION_TTL`       | `visa.cache.validation-ttl`      | Visa validation cache TTL (seconds)            | `120`          |
| `VISA_CACHE_USERINFO_TTL`         | `visa.cache.userinfo-ttl`        | Userinfo cache TTL (seconds)                   | `60`           |

### gRPC Reencrypt Service

| Variable          | Config Key        | Description                        | Default |
|-------------------|-------------------|------------------------------------|---------|
| `GRPC_HOST`       | `grpc.host`       | Reencrypt service hostname         | *required* |
| `GRPC_PORT`       | `grpc.port`       | Reencrypt service port             | `50051` |
| `GRPC_TIMEOUT`    | `grpc.timeout`    | Request timeout (seconds)          | `10`    |
| `GRPC_CACERT`     | `grpc.cacert`     | Path to CA certificate for gRPC TLS |        |
| `GRPC_CLIENT_CERT`| `grpc.client-cert` | Path to client certificate for mTLS |       |
| `GRPC_CLIENT_KEY` | `grpc.client-key`  | Path to client key for mTLS        |        |

### Storage

Storage is configured using the `storage/v2` package with the `archive` backend.

Example S3 configuration:

```yaml
storage:
  archive:
    s3:
      - endpoint: "s3.example.com"
        access_key: "access"
        secret_key: "secret"
        region: "us-east-1"
        bucket_prefix: "archive"
```

### Session

| Variable             | Config Key           | Description                      | Default       |
|----------------------|----------------------|----------------------------------|---------------|
| `SESSION_EXPIRATION` | `session.expiration` | Session expiration (seconds)     | `3600`        |
| `SESSION_DOMAIN`     | `session.domain`     | Cookie domain                    |               |
| `SESSION_SECURE`     | `session.secure`     | Use secure cookies (HTTPS only)  | `true`        |
| `SESSION_HTTP_ONLY`  | `session.http-only`  | HTTP-only cookies                | `true`        |
| `SESSION_NAME`       | `session.name`       | Session cookie name              | `sda_session` |

### Database Cache

| Variable               | Config Key             | Description                         | Default |
|------------------------|------------------------|-------------------------------------|---------|
| `CACHE_ENABLED`        | `cache.enabled`        | Enable database query caching       | `true`  |
| `CACHE_FILE_TTL`       | `cache.file-ttl`       | TTL for file queries (seconds)      | `300`   |
| `CACHE_PERMISSION_TTL` | `cache.permission-ttl` | TTL for permission checks (seconds) | `120`   |
| `CACHE_DATASET_TTL`    | `cache.dataset-ttl`    | TTL for dataset queries (seconds)   | `300`   |

### Pagination

| Variable                  | Config Key              | Description                                      | Default |
|---------------------------|-------------------------|--------------------------------------------------|---------|
| `PAGINATION_HMAC_SECRET`  | `pagination.hmac-secret`| HMAC secret for page tokens (must match across replicas) |   |

If not configured, a random secret is generated at startup. Page tokens will not
survive restarts or work across replicas without a configured secret.

### Audit

| Variable         | Config Key       | Description                               | Default |
|------------------|------------------|-------------------------------------------|---------|
| `AUDIT_REQUIRED` | `audit.required` | Require a real audit logger (fail if noop) | `false` |

### Application Environment

| Variable          | Config Key        | Description                                          | Default |
|-------------------|-------------------|------------------------------------------------------|---------|
| `APP_ENVIRONMENT` | `app.environment` | Set to `production` to enable production safety guards |        |

Production safety guards enforce:
- `jwt.allow-all-data` must be `false`
- `pagination.hmac-secret` must be configured
- `grpc.client-cert` and `grpc.client-key` must be configured

## Testing

### Unit Tests

```bash
cd sda && go test ./cmd/download/...
```

Visa-related tests use a build tag:

```bash
cd sda && go test -tags visas -count=1 ./cmd/download/...
```

### Integration Tests

```bash
make integrationtest-sda-download-v2-up    # Start test environment
make integrationtest-sda-download-v2-run   # Run tests
make integrationtest-sda-download-v2-down  # Tear down
```

### Local Development

See [TESTING.md](TESTING.md) for detailed local testing instructions including
Docker Compose setup, database seeding, and example curl commands.
