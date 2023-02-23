
DELETE FROM sda.checksums;
DELETE FROM sda.file_event_log;
DELETE FROM sda.dataset_references;
DELETE FROM sda.file_references;
DELETE FROM sda.file_dataset;
DELETE FROM sda.datasets;

DELETE FROM sda.files;

INSERT INTO sda.files(
        stable_id,
        submission_user,
        submission_file_path,
        submission_file_size,
        archive_file_path,
        archive_file_size,
        decrypted_file_size,
        backup_path,
        header,
        encryption_method
    ) VALUES (
        'testaccession01',
        'testuser01',
        's3://inbox/testfile01.c4gh',
        32768,
        's3://archive/testfile01.c4gh',
        32000,
        32000,
        's3://backup/testfile01.c4gh',
        'crypt4gh-header',
        'CRYPT4GH'
    ), (
        'testaccession02',
        'testuser01',
        's3://inbox/testfile02.c4gh',
        32768,
        's3://archive/testfile02.c4gh',
        32000,
        32000,
        's3://backup/testfile02.c4gh',
        'anothercrypt4gh-header',
        'CRYPT4GH'
    );


INSERT INTO sda.file_event_log(
        file_id,
        event
    ) VALUES (
        (SELECT id FROM sda.files WHERE stable_id = 'testaccession01'),
        'verified'
    );

INSERT INTO sda.file_event_log(
        file_id,
        event
    ) VALUES (
        (SELECT id FROM sda.files WHERE stable_id = 'testaccession02'),
        'disabled'
    );

INSERT INTO sda.checksums(
        file_id,
        checksum,
        type,
        source
    ) VALUES (
        (SELECT id FROM sda.files WHERE stable_id = 'testaccession01'),
        '55302d76b1faceaba2759e803523175c9a3e2823b81f0b5bc301fab85deb3dd3',
        'SHA256',
        'ARCHIVED'
    );
