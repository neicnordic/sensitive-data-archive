#!/bin/sh
# Seeds the download v2 integration database with the file created by
# make_download_v2_testfile.sh. Safe to rerun on a kept volume: rows derived
# from /shared are updated, so they always match the archived body.
set -eu

# shellcheck source=/dev/null
. /shared/testfile.env

# All files share the one archive object and header; they differ in path and
# checksums to cover pagination, special characters and download_path.
psql -v ON_ERROR_STOP=1 \
    -v header="$TESTFILE_HEADER" \
    -v size="$TESTFILE_SIZE" \
    -v sha256="$TESTFILE_SHA256" \
    -v md5="$TESTFILE_MD5" \
    -v body_size="$TESTFILE_BODY_SIZE" \
    -v body_sha256="$TESTFILE_BODY_SHA256" <<'EOF'
SET client_encoding = 'UTF8';

INSERT INTO sda.files (
  id, stable_id, submission_user, submission_file_path,
  archive_file_path, archive_location, archive_file_size, decrypted_file_size,
  header, encryption_method
)
SELECT f.id::uuid, f.stable_id, 'integration_test@example.org', f.path,
  'test-file.c4gh', 'http://s3:9000/archive', :body_size, :size,
  :'header', 'CRYPT4GH'
FROM (VALUES
  ('aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee', 'EGAF00000000001', 'test-file.c4gh'),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000002', 'EGAF00000000002', 'special/it''s a "quoted" fïle.c4gh'),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000003', 'EGAF00000000003', 'special/with space "and quote".c4gh'),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000004', 'EGAF00000000004', 'multi-checksum.c4gh'),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000005', 'EGAF00000000005', 'special/0-submitted-name.c4gh'),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000006', 'EGAF00000000006', 'other/submitted-name.c4gh')
) AS f(id, stable_id, path)
ON CONFLICT (id) DO UPDATE SET
  archive_location = EXCLUDED.archive_location,
  archive_file_size = EXCLUDED.archive_file_size,
  decrypted_file_size = EXCLUDED.decrypted_file_size,
  header = EXCLUDED.header;

INSERT INTO sda.checksums (file_id, checksum, type, source) VALUES
  ('aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee', :'sha256', 'SHA256', 'UNENCRYPTED'),
  ('aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee', :'body_sha256', 'SHA256', 'ARCHIVED'),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000002', :'sha256', 'SHA256', 'UNENCRYPTED'),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000002', :'body_sha256', 'SHA256', 'ARCHIVED'),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000003', :'sha256', 'SHA256', 'UNENCRYPTED'),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000003', :'body_sha256', 'SHA256', 'ARCHIVED'),
  -- Two UNENCRYPTED rows: the listing must not duplicate this file
  ('aaaaaaaa-bbbb-cccc-dddd-000000000004', :'sha256', 'SHA256', 'UNENCRYPTED'),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000004', :'md5', 'MD5', 'UNENCRYPTED'),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000004', :'body_sha256', 'SHA256', 'ARCHIVED'),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000005', :'sha256', 'SHA256', 'UNENCRYPTED'),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000005', :'body_sha256', 'SHA256', 'ARCHIVED'),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000006', :'sha256', 'SHA256', 'UNENCRYPTED'),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000006', :'body_sha256', 'SHA256', 'ARCHIVED')
ON CONFLICT ON CONSTRAINT unique_checksum DO UPDATE SET checksum = EXCLUDED.checksum;

-- id is SERIAL, so let postgres generate it
INSERT INTO sda.datasets (stable_id, title)
VALUES ('EGAD00000000001', 'Test Dataset')
ON CONFLICT DO NOTHING;

-- download_path overrides the submitted path. Under special/, EGAF...5 sorts
-- first by its submitted path and last by its download path, so listings must
-- filter, order and page on the effective path. EGAF...6 shares its effective
-- path with EGAF...4, so pagination must break the tie on stable_id.
INSERT INTO sda.file_dataset (file_id, dataset_id, download_path)
SELECT f.id::uuid, d.id, f.download_path
FROM sda.datasets d
CROSS JOIN (VALUES
  ('aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee', NULL),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000002', NULL),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000003', NULL),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000004', NULL),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000005', 'special/zz-download-name.c4gh'),
  ('aaaaaaaa-bbbb-cccc-dddd-000000000006', 'multi-checksum.c4gh')
) AS f(id, download_path)
WHERE d.stable_id = 'EGAD00000000001'
ON CONFLICT DO NOTHING;
EOF

echo 'Database seeded with test data'
