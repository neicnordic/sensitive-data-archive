# Migrating to sda-svc chart 4.0

Chart 4.0 replaces the flat scalar storage keys on `global.archive`,
`global.backupArchive`, `global.inbox`, and `global.sync.destination`
with nested `s3:` and `posix:` lists. Unlocks multi-endpoint
configurations that the service code has supported since storage-v2
(PR #2244) but the chart could not express.

This is a **breaking change**. Existing 3.x values files will not render
in 4.0 without conversion.

## Quick reference: scalar → list

### Before (3.x)

```yaml
global:
  archive:
    storageType: s3
    s3Url: https://storage.example.com
    s3Port: 443
    s3BucketPrefix: archive
    s3Region: us-east-1
    s3AccessKey: AKIA...
    s3SecretKey: ...
    s3ChunkSize: 50MB
    maxBuckets: 10
    maxObjects: 0
    maxSize: ""
    locationBrokerCacheTTL: ""
```

### After (4.0)

```yaml
global:
  archive:
    locationBrokerCacheTTL: ""    # backend-level, unchanged
    s3:
    - endpoint: https://storage.example.com
      port: 443
      bucketPrefix: archive
      region: us-east-1
      accessKey: AKIA...
      secretKey: "..."
      chunkSize: 50MB
      maxBuckets: 10              # NOTE: now per-endpoint
      maxObjects: 0               # NOTE: now per-endpoint
      maxSize: ""                 # NOTE: now per-endpoint
```

## POSIX changes

POSIX backends previously had one volume per backend with backend-level
`volumePath`, `nfsServer`, `nfsPath`, `existingClaim`. In 4.0 each POSIX
entry carries its own k8s volume backing.

### Before

```yaml
global:
  archive:
    storageType: posix
    volumePath: /archive
    existingClaim: archive-pvc
```

### After

```yaml
global:
  archive:
    posix:
    - path: /archive
      volume:
        existingClaim: archive-pvc
        # or:
        # nfsServer: nfs.example.com
        # nfsPath: /exports/archive
```

## Caution: pod volume names changed

Pod `volumes:` and `volumeMounts:` are renamed:

| 3.x volume name | 4.0 volume name |
|-----------------|-----------------|
| `archive`       | `archive-0` (and `-1`, `-2`, ... per entry) |
| `backup`        | `backup-archive-0` (note: lowercase, hyphenated) |
| `inbox`         | `inbox-0` |
| (none — sync was S3-only in 3.x) | `sync-dest-0` (new in 4.0) |

If you have any custom sidecars, init-containers, or `extraVolumes`
overlays referencing the old static names (`archive`, `backup`, `inbox`),
update them to the indexed equivalent before upgrading. The names are
lowercase DNS-1123 labels — `backupArchive-0` is not a valid k8s volume
name.

## `storageType` is removed

The `storageType: s3|posix` selector is gone. Backend rendering is now
driven by which lists are populated. A backend can declare both `s3:`
and `posix:` lists (useful for s3-writer + posix-reader migrations).

## DOA users — important

The DOA Java service does not speak the storage-v2 list format. In 4.0:

- Single-endpoint DOA deployments keep working — DOA reads index 0 of
  the `archive.s3` (or `archive.posix`) list. Credentials are sourced
  from the `<release>-s3archive-keys` Secret (populated by
  `shared-secrets.yaml`), unchanged from 3.x.
- **Multi-endpoint deployments with `global.doa.enabled: true` fail at
  chart-render time** with a clear error. DOA only sees the first
  endpoint, and silent partial coverage is a footgun. The chart blocks
  the combination explicitly.

If you depend on DOA and need multi-endpoint storage, either:

- Keep `global.archive.s3` (or `.posix`) to one entry, then plan
  separately for migrating off DOA
- Migrate away from DOA before adopting multi-endpoint

DOA is on a deprecation path; this constraint should be temporary.

### DOA endpoint URL handling

storage-v2 takes a URL with scheme (`https://host[:port]`). DOA's Java
MinIO client takes host + port + secure separately, so the chart
normalizes for it:

- `S3_ENDPOINT`: host, with the `http://` / `https://` prefix stripped
- `S3_PORT`: the port embedded in the URL > `port:` field on the list
  entry > scheme default (443 for `https`, 80 for `http`)
- `S3_SECURE`: `true` unless the scheme is `http://` (so a bare
  hostname keeps working)

A 3.x value file using `archive.s3Url: minio.minio` (no scheme) thus
keeps producing `S3_ENDPOINT=minio.minio S3_SECURE=true`.

## sftp-inbox uses `inbox.posix[0]` only

The legacy Java sftp-inbox can only mount one directory. The chart
mounts `inbox.posix[0].path` and now also sets the matching
`INBOX_LOCATION` env so the app writes to the configured path instead
of its hardcoded default (`/ega/inbox/`).

The pipeline services (api, mapper, ingest) read every entry of
`inbox.posix`. That asymmetry is intentional: sftp-inbox is the single
writer, pipeline can be multi-reader. If you do not need pipeline
multi-read, keep `inbox.posix` to one entry.

## Quota fields moved per-endpoint

`maxBuckets`, `maxObjects`, `maxSize` are now per-endpoint, matching
storage-v2's writer config. Backend-level quotas defeated sharding
(every endpoint would share the same cap), so each list entry carries
its own.

`locationBrokerCacheTTL` stays at backend level in values but is now
**rendered at config-file root** in the generated Secret (previously
nested under `storage.<backend>.location_broker.cache_ttl`, which the
service silently ignored — a pre-existing chart bug fixed in this
release). If multiple backends set `locationBrokerCacheTTL`, the
chart's per-template priority order applies (finalize prefers
backupArchive, sync prefers sync.destination, others prefer archive
or inbox).

## Multi-endpoint examples

### Two S3 endpoints (sharding by bucket-prefix)

```yaml
global:
  archive:
    s3:
    - endpoint: https://s3.example.com
      bucketPrefix: archive-a
      accessKey: ...
      secretKey: ...
      maxBuckets: 50
    - endpoint: https://s3.example.com
      bucketPrefix: archive-b
      accessKey: ...
      secretKey: ...
      maxBuckets: 50
```

Writer fills `archive-a` buckets first; spills to `archive-b` once
`archive-a` hits `maxBuckets`.

### Mixed s3 + posix (writer s3, reader posix)

For the **writer backends** (`global.archive`, `global.backupArchive`,
`global.sync.destination`), storage-v2 supports only **one writer
implementation** — either S3 or POSIX, never both. If both lists
contain writer-enabled entries the runtime returns
`ErrorMultipleWritersNotSupported`. The chart fails fast at
`helm template` time with:

```
global.archive has writer-enabled entries in both s3 and posix.
storage-v2 supports only one writer implementation per backend
(ErrorMultipleWritersNotSupported). Set writerDisabled: true on all
but one writer; readers are unaffected.
```

Mark every endpoint on the non-writer side with `writerDisabled: true`:

```yaml
global:
  archive:
    s3:
    - endpoint: https://s3.example.com
      bucketPrefix: archive
      accessKey: ...
      secretKey: ...
    posix:
    - path: /legacy-archive
      writerDisabled: true     # reader only
      volume:
        existingClaim: legacy-archive-pvc
```

### Two POSIX shards on different NFS servers

```yaml
global:
  archive:
    posix:
    - path: /archive_1
      maxSize: 10TB
      volume:
        nfsServer: nfs1.example.com
        nfsPath: /exports/archive
    - path: /archive_2
      volume:
        nfsServer: nfs2.example.com
        nfsPath: /exports/archive
```

## Verifying the migration

Run `helm template` against your converted values; if any `required`
check fails it will name the missing field, e.g.:

```
Error: ...: global.archive.s3[0].endpoint is required
Error: ...: global.archive.posix[1].volume.nfsServer is required (or set volume.existingClaim)
```

Then `helm diff upgrade` against an installed release will show the
secret rewrites and volume renaming.
