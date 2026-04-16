package postgres

import (
	"context"
	"database/sql"
)

const getInboxPathQuery = "getInboxPath"

func init() {
	queries[getInboxPathQuery] = `
SELECT submission_file_path 
FROM sda.files 
WHERE stable_id = $1;
`
}

func (db *pgDb) getInboxPath(ctx context.Context, tx *sql.Tx, accessionID string) (string, error) {
	stmt := db.getPreparedStmt(tx, getInboxPathQuery)

	var inboxPath string
	err := stmt.QueryRowContext(ctx, accessionID).Scan(&inboxPath)
	if err != nil {
		return "", err
	}

	return inboxPath, nil
}
