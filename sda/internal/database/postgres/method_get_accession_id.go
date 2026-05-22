package postgres

import (
	"context"
	"database/sql"
)

const getAccessionIDQuery = "getAccessionID"

func init() {
	queries[getAccessionIDQuery] = `
SELECT stable_id 
FROM sda.files 
WHERE id = $1;
`
}

func (db *pgDb) getAccessionID(ctx context.Context, tx *sql.Tx, fileID string) (string, error) {
	stmt, err := db.getPreparedStmt(tx, getAccessionIDQuery)
	if err != nil {
		return "", err
	}

	var aID string
	if err := stmt.QueryRowContext(ctx, fileID).Scan(&aID); err != nil {
		return "", err
	}

	return aID, nil
}
