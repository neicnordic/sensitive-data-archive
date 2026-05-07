#!/bin/sh
# Seed script for download v2 dev compose.
# Creates a real Crypt4GH encrypted test file, uploads data segments to MinIO,
# and inserts matching metadata (header, checksums, sizes) into the database.
# Idempotent: safe to re-run against existing volumes.
set -e

echo "=== Seeding test data ==="

# Create deterministic plaintext test data (1 KB)
python3 -c "import hashlib; open('/tmp/plaintext.bin','wb').write(hashlib.sha256(b'sda-download-v2-dev-test-data').digest() * 32)"

# Encrypt with crypt4gh using the server's public key
crypt4gh encrypt --recipient_pk /shared/c4gh.pub.pem < /tmp/plaintext.bin > /tmp/encrypted.c4gh

# Split: extract header and data segments, compute checksums
python3 << 'PYEOF'
import struct, hashlib

with open("/tmp/encrypted.c4gh", "rb") as f:
    data = f.read()

# Parse c4gh header
magic = data[:8]
assert magic == b"crypt4gh", f"Bad magic: {magic}"
packet_count = struct.unpack("<I", data[12:16])[0]

offset = 16
for _ in range(packet_count):
    pkt_len = struct.unpack("<I", data[offset:offset+4])[0]
    offset += pkt_len

header = data[:offset]
body = data[offset:]

with open("/tmp/header.bin", "wb") as f:
    f.write(header)
with open("/tmp/body.bin", "wb") as f:
    f.write(body)

plaintext = open("/tmp/plaintext.bin", "rb").read()

with open("/tmp/seed_metadata.env", "w") as f:
    f.write(f"HEADER_HEX={header.hex()}\n")
    f.write(f"ARCHIVE_SIZE={len(body)}\n")
    f.write(f"DECRYPTED_SIZE={len(plaintext)}\n")
    f.write(f"ARCHIVE_CHECKSUM={hashlib.sha256(body).hexdigest()}\n")
    f.write(f"DECRYPTED_CHECKSUM={hashlib.sha256(plaintext).hexdigest()}\n")

print(f"Header: {len(header)} bytes, Body: {len(body)} bytes")
PYEOF

# Upload data segments to MinIO (overwrites if exists)
mc alias set myminio http://s3:9000 access secretKey --quiet
mc mb myminio/archive --ignore-existing --quiet
mc pipe myminio/archive/test-file.c4gh < /tmp/body.bin
echo "Uploaded to MinIO: archive/test-file.c4gh"

# Load computed metadata
# shellcheck source=/dev/null
. /tmp/seed_metadata.env

# Seed database (upserts so reruns are safe with persistent volumes)
pg_isready -h postgres -p 5432 -U postgres

psql -h postgres -U postgres -d sda << EOSQL
INSERT INTO sda.files (
  id, stable_id, submission_user, submission_file_path,
  archive_file_path, archive_location, archive_file_size, decrypted_file_size,
  header, encryption_method
) VALUES (
  'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee',
  'EGAF00000000001',
  'integration_test@example.org',
  'test-file.c4gh',
  'test-file.c4gh',
  'http://s3:9000/archive',
  $ARCHIVE_SIZE,
  $DECRYPTED_SIZE,
  '$HEADER_HEX',
  'CRYPT4GH'
) ON CONFLICT (id) DO UPDATE SET
  archive_file_size = EXCLUDED.archive_file_size,
  decrypted_file_size = EXCLUDED.decrypted_file_size,
  header = EXCLUDED.header;

INSERT INTO sda.datasets (stable_id, title)
  VALUES ('EGAD00000000001', 'Test Dataset')
  ON CONFLICT (stable_id) DO NOTHING;

INSERT INTO sda.file_dataset (file_id, dataset_id)
  SELECT 'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee'::uuid, d.id
  FROM sda.datasets d
  WHERE d.stable_id = 'EGAD00000000001'
  ON CONFLICT DO NOTHING;

DELETE FROM sda.checksums WHERE file_id = 'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee';
INSERT INTO sda.checksums (file_id, checksum, type, source) VALUES
  ('aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee', '$ARCHIVE_CHECKSUM', 'SHA256', 'ARCHIVED'),
  ('aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee', '$DECRYPTED_CHECKSUM', 'SHA256', 'UNENCRYPTED');
EOSQL

echo "=== Seed complete ==="
