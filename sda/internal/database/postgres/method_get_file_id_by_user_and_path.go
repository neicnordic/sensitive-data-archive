package postgres

import (
	"context"
	"database/sql"
)

const getFileIDByUserAndPathQuery = "getFileIDByUserAndPath"

func init() {
	queries[getFileIDByUserAndPathQuery] = `SELECT id FROM sda.files AS f
    WHERE f.submission_user = $1
    AND f.submission_file_path = $2
    AND f.last_event != 'disabled';`
}

// getFileIDByUserAndPath returns the currently active (not disabled) file for a given
// user and submission path, regardless of its status or whether it has an accession id.
func (db *pgDb) getFileIDByUserAndPath(ctx context.Context, tx *sql.Tx, submissionUser, filePath string) (string, error) {
	stmt, err := db.getPreparedStmt(tx, getFileIDByUserAndPathQuery)
	if err != nil {
		return "", err
	}

	var fileID string

	if err := stmt.QueryRowContext(ctx, submissionUser, filePath).Scan(&fileID); err != nil {
		return "", err
	}

	return fileID, nil
}
