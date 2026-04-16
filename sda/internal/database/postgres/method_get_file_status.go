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
	stmt := db.getPreparedStmt(tx, getFileStatusQuery)

	var status string
	err := stmt.QueryRowContext(ctx, fileID).Scan(&status)
	if err != nil {
		return "", err
	}

	return status, nil
}
