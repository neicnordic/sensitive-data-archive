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

### Migrating from v1 (sda-download)

This service replaces the standalone [sda-download](https://github.com/neicnordic/sda-download)
Go service. The HTTP surface has been reorganised; the table below maps the
common v1 endpoints to their v2 equivalents.

#### Endpoint mapping

| v1 endpoint                                | v2 endpoint                                  | Notes                                                                                   |
|--------------------------------------------|----------------------------------------------|-----------------------------------------------------------------------------------------|
| `GET /metadata/datasets`                   | `GET /datasets`                              | Returns a paginated envelope `{ "datasets": [ids...], "nextPageToken": ... }`, not a flat array of strings |
| `GET /metadata/datasets/{ds}/files`        | `GET /datasets/:datasetId/files`             | Paginated; response shape changed (see below)                                           |
| `GET /files/{fileId}`                      | `GET /files/:fileId`                         | Path unchanged, but always returns a Crypt4GH file re-encrypted to the recipient's public key. v1 could be configured to stream plaintext from `/files/...` when the service held a Crypt4GH private key; that mode no longer exists. See [Decrypted streaming has been removed](#decrypted-streaming-has-been-removed). Range support via HTTP `Range` header only. |
| _Not in v1_                                | `HEAD /files/:fileId`                        | v1 only supported `HEAD` on `/s3/*path`. v2 adds `HEAD` to every download tier (`/files/:fileId`, `/files/:fileId/header`, `/files/:fileId/content`). |
| `GET /s3/{datasetid}/{fileid}` (decrypted) | _Removed_                                    | Decrypted streaming is no longer offered. Clients decrypt locally with their c4gh key.  |
| `GET /s3-encrypted/{datasetid}/{fileid}`   | `GET /files/:fileId`                         | v1 prepended a re-encrypted Crypt4GH header before the body, so the closest complete `.c4gh` equivalent is the combined endpoint. Clients that fetch the header and body separately can use `/files/:fileId/header` + `/files/:fileId/content` instead. |
| _New in v2_                                | `GET /files/:fileId/header`                  | Re-encrypted Crypt4GH header only — useful for htsget-style clients that fetch the header once and stream content separately. |
| _New in v2_                                | `GET /objects/:datasetId/:filePath`          | GA4GH DRS 1.5 object endpoint. Returns checksums of the **encrypted** blob (per DRS).   |

#### File listing response shape

v1 returned a flat array; v2 wraps the array in an envelope and renames a few
fields:

| v1 field                        | v2 field                              | Notes                                                                                |
|---------------------------------|---------------------------------------|--------------------------------------------------------------------------------------|
| `fileId`                        | `fileId`                              | Unchanged                                                                            |
| `filePath`                      | `filePath`                            | Unchanged                                                                            |
| `displayFileName`               | _Removed_                             | Derive from `filePath` if needed (final path segment)                                |
| `decryptedFileSize`             | `decryptedSize`                       | Renamed                                                                              |
| _(not in v1)_                   | `size`                                | Archive blob size, excluding the Crypt4GH header. Matches the bytes served at `/files/:fileId/content`. The combined `/files/:fileId` response is larger by the re-encrypted header. |
| `decryptedFileChecksum` + `decryptedFileChecksumType` | `checksums[]` array | Same source (`UNENCRYPTED`), array shape allows multiple algorithms — see [Checksums](#checksums) |
| _(not in v1)_                   | `downloadUrl`                         | Convenience pointer to `/files/:fileId`                                              |
| _(top-level)_                   | `nextPageToken`                       | Pagination cursor — see [Pagination](#pagination) below                              |

#### Other behavioural changes

- **Public-key header renamed (and a second accepted name added).** v1 used
  `Client-public-key`. v2 accepts the recipient public key (base64-encoded) on
  exactly one of two headers: `X-C4GH-Public-Key` (preferred) or
  `Htsget-Context-Public-Key` (for htsget-style clients). Supplying both
  returns `400` with code `KEY_CONFLICT`; omitting both on an endpoint that
  needs the key returns `400` with code `KEY_MISSING`. v1's separate
  `?scheme=` query parameter has been removed — encode dataset IDs as path
  segments directly.
- **Range parameters removed.** v1 accepted `?startCoordinate=&endCoordinate=`
  query parameters as well as the HTTP `Range` header. v2 only honours the
  HTTP `Range` header, in the standard single byte-range forms:
  `bytes=START-END`, `bytes=START-`, and `bytes=-SUFFIX`. Multi-range
  requests (comma-separated ranges) are not supported and return `400`.
- **`ETag` semantics changed.** v1 set `ETag` to the plaintext SHA-256 of the
  file (i.e. the same value as `decryptedFileChecksum`). v2 sets `ETag`
  differently per endpoint, and in neither case is it the plaintext checksum:
  - `GET /files/:fileId` (combined header + body) returns a SHA-256 of the
    re-encrypted Crypt4GH header. Because the re-encryption step generates a
    fresh ephemeral X25519 keypair on every call, the header bytes change on
    every request, so this `ETag` changes per request and is not usable for
    `If-Range` resume across requests.
  - `GET /files/:fileId/content` returns a stable synthetic ETag derived from
    `(fileId, archiveSize)`. It identifies the resource but is not a checksum
    of the returned bytes; the returned bytes are the header-stripped archive
    blob and do match the `ARCHIVED` checksum (see [Checksums](#checksums)).

  Use `checksums[]` from `/datasets/:ds/files` for plaintext verification
  after decryption.
- **Session cookie default renamed** from `sda_session_key` to `sda_session`. The
  legacy name is still accepted for backwards compatibility — see
  [Session Caching](#session-caching).
- **`X-Amz-Security-Token` accepted** alongside `Authorization: Bearer` to support
  S3-style clients. v1 only accepted `Authorization`.

#### Decrypted streaming has been removed

v1 could stream the plaintext file directly when the service was deployed
with a Crypt4GH private key. This applied both to `/s3/{datasetid}/{fileid}`
(the default `type` arm) and to `/files/{fileid}`, which shared the same
handler. v2 does not. Every download returns Crypt4GH-encrypted bytes
re-encrypted to the recipient's public key, and the client decrypts
locally.

If a v1 deployment used server-side decryption — whether through an htsget
proxy reading plaintext off `/s3/...`, or clients pulling plaintext from
`/files/{fileid}` — the operator has two options: move decryption to the
consumer (the consumer generates its own Crypt4GH keypair, sends the public
key on one of the accepted public-key headers (`X-C4GH-Public-Key` or
`Htsget-Context-Public-Key`), and decrypts locally with its own private
key), or run a decrypting proxy in front of the consumer that holds a
Crypt4GH keypair on its behalf. The archive's private key is never shipped
to clients.

v1 also returned `400 Bad Request` from `/s3/...` when
`ALLOW_UNENCRYPTED_DOWNLOAD` was off. v2 has no equivalent path; the
`/files/:fileId` flow always returns encrypted bytes, so any v1 client
branching on that `400` can drop the branch.

#### Dataset IDs that contain a URL scheme

v1 documented a `?scheme=https` query parameter to work around reverse-proxy
issues with dataset IDs like `https://doi.org/abc/123`. v2 enables
`router.UseRawPath`, so the dataset ID is passed as a single URL-encoded path
segment:

```bash
# v1: /metadata/datasets/doi.org/abc/123/files?scheme=https
# v2: /datasets/https%3A%2F%2Fdoi.org%2Fabc%2F123/files
curl -H "Authorization: Bearer $token" \
     "https://HOSTNAME/datasets/https%3A%2F%2Fdoi.org%2Fabc%2F123/files"
```

The router matches routes against the raw URL-encoded path, then
`c.Param("datasetId")` returns the decoded value. v2 does not recognise the
`?scheme=` query parameter.

#### Pagination

v1 list endpoints returned a single response with the full result set. v2
returns paginated envelopes:

```json
{
  "datasets": [ ... ],
  "nextPageToken": "ptk_7f9K2mQxVb3N"
}
```

If `nextPageToken` is non-null, pass it as `?pageToken=<token>` to retrieve
the next page. The `pageSize` parameter is optional and bounded server-side.
Page tokens are HMAC-signed and tied to the originating query — do not modify
or reuse them across different queries.

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
  - `pageSize` (optional): Number of results per page
  - `pageToken` (optional): Opaque token for the next page

- Response: JSON envelope `{ "datasets": [...string IDs], "nextPageToken": ... }`
- Error codes
  - `200` Success
  - `400` Invalid `pageSize`, or malformed/expired/mismatched `pageToken`
    (including a `pageSize` change between pages)
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
  - `pageSize` (optional): Number of results per page
  - `pageToken` (optional): Opaque token for the next page
  - `filePath` (optional): Exact dataset-relative file path. Returns at
    most one result.
  - `pathPrefix` (optional): Recursive prefix filter (for example
    `samples/controls/`).
  - `filePath` and `pathPrefix` are mutually exclusive; supplying both
    returns `400` with code `FILTER_CONFLICT`. Either filter value over
    4096 characters also returns `400`.

- Error codes
  - `200` Success
  - `400` Invalid pagination parameters, malformed/expired/mismatched
    `pageToken`, or conflicting/oversized filters
  - `401` Invalid or missing token
  - `403` Access denied or dataset does not exist

Example:

```bash
curl -H "Authorization: Bearer $token" https://HOSTNAME/datasets/EGAD00000000001/files
```

Response:

```json
{
  "files": [
    {
      "fileId": "EGAF00000000001",
      "filePath": "samples/sample1.bam.c4gh",
      "size": 1048576,
      "decryptedSize": 1048512,
      "checksums": [
        {"type": "sha256", "checksum": "7d2c8b4a..."}
      ],
      "downloadUrl": "/files/EGAF00000000001"
    }
  ],
  "nextPageToken": null
}
```

`checksums[]` are over the decrypted (plaintext) file content. This is the
value to verify against after decrypting the downloaded `.c4gh` file. It
replaces v1's `decryptedFileChecksum` / `decryptedFileChecksumType` fields,
shaped as an array so a file can carry multiple algorithms.

See [Checksums](#checksums) for the differences between this endpoint and
the DRS object endpoint.

### File Endpoints

All file endpoints require authentication. Endpoints that produce a re-encrypted
Crypt4GH header — `GET /files/:fileId` (and `HEAD`) and `GET /files/:fileId/header`
(and `HEAD`) — additionally require a base64-encoded Crypt4GH public key on
exactly one of two request headers:

- `X-C4GH-Public-Key` (preferred)
- `Htsget-Context-Public-Key` (htsget-compatible alternative)

Supplying both returns `400` with code `KEY_CONFLICT`. Omitting both returns
`400` with code `KEY_MISSING`. The `/files/:fileId/content` endpoint streams
pre-encrypted archive bytes unchanged and does not require a public key.

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
  - Exactly one of `X-C4GH-Public-Key` or `Htsget-Context-Public-Key`
    (required), value is the base64-encoded recipient Crypt4GH public key
  - `Range` (optional): a single HTTP byte range — `bytes=START-END`,
    `bytes=START-`, or `bytes=-SUFFIX`. Multi-range requests are rejected.

- Response Headers
  - `Content-Type: application/octet-stream`
  - `Content-Disposition: attachment; filename="<basename>.c4gh"` — `<basename>`
    is the final path segment of the file's `submission_file_path`. The handler
    appends `.c4gh` only if it is not already present.
  - `Accept-Ranges: bytes`

- Error codes
  - `200` Download successful
  - `206` Partial content (Range request)
  - `400` Missing or conflicting public key header (`KEY_MISSING` /
    `KEY_CONFLICT`), or invalid/multi-range `Range`
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

- Response Headers
  - `Content-Length`: size of the re-encrypted Crypt4GH header in bytes
  - `SDA-Content-ETag`: the same stable synthetic ETag that
    `/files/:fileId/content` returns as its `ETag`. Lets a client fetch the
    header and the content in two separate requests and confirm that they
    refer to the same archive blob.

#### `HEAD /files/:fileId/content` and `GET /files/:fileId/content`

Returns only the encrypted data segments (without the Crypt4GH header).
Range byte offsets refer to the data segments only. The ETag is stable for a
given file and independent of the recipient key.

Example with Range:

```bash
curl -H "Authorization: Bearer $token" \
     -H "Range: bytes=0-65535" \
     https://HOSTNAME/files/EGAF00000000001/content
```

### Checksums

The service exposes two distinct checksum values for each file. Pick the one
that matches what your client is verifying:

| Endpoint                          | Source        | Describes                                    | Use for                                       |
|-----------------------------------|---------------|----------------------------------------------|-----------------------------------------------|
| `GET /datasets/:ds/files`         | `UNENCRYPTED` | Decrypted (plaintext) file content           | End-user integrity check after decrypting     |
| `GET /objects/:ds/:filePath` (DRS)| `ARCHIVED`    | Encrypted blob in archive (per DRS 1.5 spec) | DRS-aware tooling (htsget-rs, etc.)           |

For users downloading a file and verifying it locally, the value to compare
against is the `UNENCRYPTED` checksum returned by `/datasets/:ds/files`. The
`ARCHIVED` checksum exists to satisfy GA4GH DRS 1.5, where `size` and
`checksums` must describe the bytes served at `access_url`.

**v1 to v2 mapping:** if you previously read `decryptedFileChecksum` from v1's
`/metadata/datasets/{ds}/files`, the v2 equivalent is `checksums[]` on
`/datasets/:ds/files`. Same source, just shaped as an array.

**Which bytes match which checksum:**

- `GET /files/:fileId/content` streams the header-stripped archive blob.
  These bytes match the `ARCHIVED` checksum (the same value the DRS
  endpoint exposes). Its `ETag` is a stable synthetic value derived from
  `(fileId, archiveSize)` and is not itself a hash of the returned bytes.
- `GET /files/:fileId` (combined) prepends a Crypt4GH header re-encrypted
  to the recipient's public key. Re-encryption uses a fresh ephemeral
  keypair on every call, so the header bytes — and therefore the wire
  bytes — change on every request, and match neither stored checksum.
  Its `ETag` is the SHA-256 of that re-encrypted header only, not a
  full-file digest.
- After decrypting locally, verify against the `UNENCRYPTED` checksum from
  `/datasets/:ds/files`.

### DRS Object Endpoint

#### `GET /objects/{datasetId}/{filePath}`

Returns a minimal [GA4GH DRS 1.5](https://ga4gh.github.io/data-repository-service-schemas/preview/release/drs-1.5.0/docs/)
`DrsObject` with a pre-resolved `access_url` pointing to the file content endpoint.
This enables DRS-aware clients (e.g. htsget-rs) to resolve a dataset + file path
to a download URL without knowing the internal file ID.

The path is composite: everything before the first `/` is the dataset ID, everything
after is the file path within the dataset.

> **Limitation with URL-like dataset IDs.** The handler splits the (decoded)
> path at its first `/`. Dataset IDs that contain slashes after decoding
> (such as `https://doi.org/...`) cannot currently be addressed via this
> endpoint, because the split point is ambiguous. The non-DRS
> `/datasets/:datasetId/files` route does not have this problem and accepts
> a URL-encoded dataset ID as a single path segment (see [Dataset IDs that
> contain a URL scheme](#dataset-ids-that-contain-a-url-scheme)).

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
