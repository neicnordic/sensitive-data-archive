package postgres

import (
	"context"
	"database/sql"
	"errors"
)

const getFileIDInInboxQuery = "getFileIDInInbox"

func init() {
	queries[getFileIDInInboxQuery] = `
SELECT id_and_event.id
FROM (
    SELECT DISTINCT ON (f.id) f.id, fel.event FROM sda.files AS f
        LEFT JOIN sda.file_event_log AS fel ON fel.file_id = f.id
    WHERE f.submission_user = $1
      AND f.submission_file_path = $2
      AND f.archive_file_path = ''
    ORDER BY f.id, fel.started_at DESC LIMIT 1
    ) AS id_and_event
WHERE id_and_event.event IN ('registered', 'uploaded', 'disabled');`
}

func (db *pgDb) getFileIDInInbox(ctx context.Context, tx *sql.Tx, submissionUser, filePath string) (string, error) {
	stmt := db.getPreparedStmt(tx, getFileIDInInboxQuery)

	var fileID string

	if err := stmt.QueryRowContext(ctx, submissionUser, filePath).Scan(&fileID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}

		return "", err
	}

	return fileID, nil
}
