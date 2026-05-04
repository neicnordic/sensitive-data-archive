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
	stmt := db.getPreparedStmt(tx, getAccessionIDQuery)

	var aID string
	err := stmt.QueryRowContext(ctx, fileID).Scan(&aID)
	if err != nil {
		return "", err
	}

	return aID, nil
}
