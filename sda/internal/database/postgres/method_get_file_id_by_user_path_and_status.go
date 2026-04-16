package postgres

import (
	"context"
	"database/sql"
)

const getFileIDByUserPathAndStatusQuery = "getFileIDByUserPathAndStatus"

func init() {
	queries[getFileIDByUserPathAndStatusQuery] = `
SELECT id_and_event.id
FROM (
    SELECT DISTINCT ON (f.id) f.id, fel.event FROM sda.files AS f
        LEFT JOIN sda.file_event_log AS fel ON fel.file_id = f.id
    WHERE f.submission_user = $1
      AND f.submission_file_path = $2
      AND f.stable_id IS NULL
    ORDER BY f.id, fel.started_at DESC LIMIT 1
    ) AS id_and_event
WHERE id_and_event.event = $3;
`
}

func (db *pgDb) getFileIDByUserPathAndStatus(ctx context.Context, tx *sql.Tx, submissionUser, filePath, status string) (string, error) {
	stmt := db.getPreparedStmt(tx, getFileIDByUserPathAndStatusQuery)

	var fileID string
	err := stmt.QueryRowContext(ctx, submissionUser, filePath, status).Scan(&fileID)
	if err != nil {
		return "", err
	}

	return fileID, nil
}
