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
	stmt, err := db.getPreparedStmt(tx, getInboxPathQuery)
	if err != nil {
		return "", err
	}

	var inboxPath string
	if err := stmt.QueryRowContext(ctx, accessionID).Scan(&inboxPath); err != nil {
		return "", err
	}

	return inboxPath, nil
}
