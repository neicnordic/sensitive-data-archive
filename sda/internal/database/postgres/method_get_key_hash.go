package postgres

import (
	"context"
	"database/sql"
)

const getKeyHashQuery = "getKeyHash"

func init() {
	queries[getKeyHashQuery] = `
SELECT key_hash 
FROM sda.files 
WHERE id = $1;
`
}

func (db *pgDb) getKeyHash(ctx context.Context, tx *sql.Tx, fileID string) (string, error) {
	stmt := db.getPreparedStmt(tx, getKeyHashQuery)

	var keyHash string
	err := stmt.QueryRowContext(ctx, fileID).Scan(&keyHash)
	if err != nil {
		return "", err
	}

	return keyHash, nil
}
