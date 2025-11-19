
SET search_path TO sda;

-- When there is an update, update the last_modified and last_modified_by
-- fields on the files table.
CREATE FUNCTION files_updated()
RETURNS TRIGGER AS $files_updated$
BEGIN
    NEW.last_modified = clock_timestamp();
    NEW.last_modified_by = CURRENT_USER;
	RETURN NEW;
END;
$files_updated$ LANGUAGE plpgsql;

CREATE TRIGGER files_last_modified
    BEFORE UPDATE ON sda.files
    FOR EACH ROW
    EXECUTE PROCEDURE files_updated();

-- Function for registering files on upload
CREATE FUNCTION sda.register_file(file_id TEXT, submission_file_path TEXT, submission_user TEXT)
    RETURNS TEXT AS $register_file$
DECLARE
    file_uuid UUID;
BEGIN
    -- Upsert file information. we're not interested in restarted uploads so old
    -- overwritten files that haven't been ingested are updated instead of
    -- inserting a new row.
INSERT INTO sda.files( id, submission_file_path, submission_user, encryption_method )
VALUES(  COALESCE(CAST(NULLIF(file_id, '') AS UUID), gen_random_uuid()), submission_file_path, submission_user, 'CRYPT4GH' )
    ON CONFLICT ON CONSTRAINT unique_ingested
    DO UPDATE SET submission_file_path = EXCLUDED.submission_file_path,
           submission_user = EXCLUDED.submission_user,
           encryption_method = EXCLUDED.encryption_method
           RETURNING id INTO file_uuid;

-- We add a new event for every registration though, as this might help for
-- debugging.
INSERT INTO sda.file_event_log( file_id, event, user_id )
VALUES (file_uuid, 'registered', submission_user);

RETURN file_uuid;
END;
$register_file$ LANGUAGE plpgsql;


CREATE FUNCTION set_archived(file_uuid UUID, corr_id UUID, file_path TEXT, file_size BIGINT, inbox_checksum_value TEXT, inbox_checksum_type TEXT)
RETURNS void AS $set_archived$
BEGIN
    UPDATE sda.files SET archive_file_path = file_path, archive_file_size = file_size WHERE id = file_uuid;

    INSERT INTO sda.checksums(file_id, checksum, type, source)
    VALUES(file_uuid, inbox_checksum_value, upper(inbox_checksum_type)::sda.checksum_algorithm, upper('UPLOADED')::sda.checksum_source);

    INSERT INTO sda.file_event_log(file_id, event, correlation_id) VALUES(file_uuid, 'archived', corr_id);
END;

$set_archived$ LANGUAGE plpgsql;

CREATE FUNCTION set_verified(file_uuid UUID, corr_id UUID, archive_checksum TEXT, archive_checksum_type TEXT, decrypted_size BIGINT, decrypted_checksum TEXT, decrypted_checksum_type TEXT)
RETURNS void AS $set_verified$
BEGIN
    UPDATE sda.files SET decrypted_file_size = decrypted_size WHERE id = file_uuid;

    INSERT INTO sda.checksums(file_id, checksum, type, source)
    VALUES(file_uuid, archive_checksum, upper(archive_checksum_type)::sda.checksum_algorithm, upper('ARCHIVED')::sda.checksum_source);

    INSERT INTO sda.checksums(file_id, checksum, type, source)
    VALUES(file_uuid, decrypted_checksum, upper(decrypted_checksum_type)::sda.checksum_algorithm, upper('UNENCRYPTED')::sda.checksum_source);

    INSERT INTO sda.file_event_log(file_id, event, correlation_id) VALUES(file_uuid, 'verified', corr_id);
END;

$set_verified$ LANGUAGE plpgsql;