package postgres

import (
	"context"
	"database/sql"
)

const getFileStatusQuery = "getFileStatus"

func init() {
	queries[getFileStatusQuery] = `
SELECT event 
FROM sda.file_event_log 
WHERE file_id = $1 
ORDER BY id DESC LIMIT 1;
`
}

func (db *pgDb) getFileStatus(ctx context.Context, tx *sql.Tx, fileID string) (string, error) {
	stmt, err := db.getPreparedStmt(tx, getFileStatusQuery)
	if err != nil {
		return "", err
	}

	var status string
	if err := stmt.QueryRowContext(ctx, fileID).Scan(&status); err != nil {
		return "", err
	}

	return status, nil
}
