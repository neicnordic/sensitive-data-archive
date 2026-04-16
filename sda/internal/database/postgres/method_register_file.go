package postgres

import (
	"context"
	"database/sql"
)

const registerFileQuery = "registerFile"

func init() {
	queries[registerFileQuery] = `
INSERT INTO sda.files(id, submission_location, submission_file_path, submission_user, encryption_method)
VALUES(COALESCE(CAST(NULLIF($1, '') AS UUID), gen_random_uuid()), $2, $3, $4, 'CRYPT4GH' )
    ON CONFLICT ON CONSTRAINT unique_ingested
    DO UPDATE SET submission_location = EXCLUDED.submission_location,
           submission_file_path = EXCLUDED.submission_file_path,
           submission_user = EXCLUDED.submission_user,
           encryption_method = EXCLUDED.encryption_method
	RETURNING id;
`
}

func (db *pgDb) registerFile(ctx context.Context, tx *sql.Tx, fileID *string, inboxLocation, uploadPath, uploadUser string) (string, error) {
	stmt := db.getPreparedStmt(tx, registerFileQuery)

	logFileEventStmt := db.getPreparedStmt(tx, updateFileEventLogQuery)

	var createdFileID string
	fileIDArg := sql.NullString{}
	if fileID != nil {
		fileIDArg.Valid = true
		fileIDArg.String = *fileID
	}

	if err := stmt.QueryRowContext(ctx, fileIDArg, inboxLocation, uploadPath, uploadUser).Scan(&createdFileID); err != nil {
		return "", err
	}

	if _, err := logFileEventStmt.ExecContext(ctx, createdFileID, "registered", uploadUser, nil, nil); err != nil {
		return "", err
	}

	return createdFileID, nil
}
