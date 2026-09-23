#!/bin/sh
# Seeds the download v2 integration database with the file created by
# make_download_v2_testfile.sh. Safe to rerun on a kept volume: rows derived
# from /shared are updated, so they always match the archived body.
set -e

# shellcheck source=/dev/null
. /shared/testfile.env

psql -v ON_ERROR_STOP=1 \
    -v header="$TESTFILE_HEADER" \
    -v size="$TESTFILE_SIZE" \
    -v sha256="$TESTFILE_SHA256" \
    -v body_size="$TESTFILE_BODY_SIZE" \
    -v body_sha256="$TESTFILE_BODY_SHA256" <<'EOF'
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
  :body_size,
  :size,
  :'header',
  'CRYPT4GH'
) ON CONFLICT (id) DO UPDATE SET
  archive_location = EXCLUDED.archive_location,
  archive_file_size = EXCLUDED.archive_file_size,
  decrypted_file_size = EXCLUDED.decrypted_file_size,
  header = EXCLUDED.header;

INSERT INTO sda.checksums (file_id, checksum, type, source) VALUES
  ('aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee', :'sha256', 'SHA256', 'UNENCRYPTED'),
  ('aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee', :'body_sha256', 'SHA256', 'ARCHIVED')
ON CONFLICT ON CONSTRAINT unique_checksum DO UPDATE SET checksum = EXCLUDED.checksum;

-- id is SERIAL, so let postgres generate it
INSERT INTO sda.datasets (stable_id, title)
VALUES ('EGAD00000000001', 'Test Dataset')
ON CONFLICT DO NOTHING;

INSERT INTO sda.file_dataset (file_id, dataset_id)
SELECT 'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee'::uuid, d.id
FROM sda.datasets d
WHERE d.stable_id = 'EGAD00000000001'
ON CONFLICT DO NOTHING;
EOF

echo 'Database seeded with test data'
