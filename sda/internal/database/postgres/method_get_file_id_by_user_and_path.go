package postgres

import (
	"context"
	"database/sql"
)

const getFileIDByUserAndPathQuery = "getFileIDByUserAndPath"

func init() {
	queries[getFileIDByUserAndPathQuery] = `SELECT id_and_event.id
FROM (
    SELECT DISTINCT ON (f.id) f.id, fel.event, fel.started_at FROM sda.files AS f
        LEFT JOIN sda.file_event_log AS fel ON fel.file_id = f.id
    WHERE f.submission_user = $1
      AND f.submission_file_path = $2
    ORDER BY f.id, fel.started_at DESC
    ) AS id_and_event
WHERE id_and_event.event != 'disabled'
ORDER BY id_and_event.started_at DESC
LIMIT 1;`
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
