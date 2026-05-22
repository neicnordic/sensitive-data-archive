package postgres

import (
	"context"
	"database/sql"
	"encoding/hex"
)

const getHeaderByAccessionIDQuery = "getHeaderByAccessionID"

func init() {
	queries[getHeaderByAccessionIDQuery] = `
SELECT header 
FROM sda.files 
WHERE stable_id = $1;
`
}

func (db *pgDb) getHeaderByAccessionID(ctx context.Context, tx *sql.Tx, accessionID string) ([]byte, error) {
	stmt, err := db.getPreparedStmt(tx, getHeaderByAccessionIDQuery)
	if err != nil {
		return nil, err
	}

	var hexString string
	if err := stmt.QueryRowContext(ctx, accessionID).Scan(&hexString); err != nil {
		return nil, err
	}

	header, err := hex.DecodeString(hexString)
	if err != nil {
		return nil, err
	}

	return header, nil
}
