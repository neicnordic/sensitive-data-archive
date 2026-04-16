package postgres

import (
	"context"
	"database/sql"
)

const setSubmissionFileSizeQuery = "setSubmissionFileSize"

func init() {
	queries[setSubmissionFileSizeQuery] = `
UPDATE sda.files 
SET submission_file_size = $1 
WHERE id = $2;
`
}

func (db *pgDb) setSubmissionFileSize(ctx context.Context, tx *sql.Tx, fileID string, submissionFileSize int64) error {
	stmt := db.getPreparedStmt(tx, setSubmissionFileSizeQuery)

	r, err := stmt.ExecContext(ctx, submissionFileSize, fileID)
	if err != nil {
		return err
	}

	rows, err := r.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}
