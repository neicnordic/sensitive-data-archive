package postgres

import (
	"context"
	"database/sql"
	"encoding/hex"
)

const getHeaderQuery = "getHeader"

func init() {
	queries[getHeaderQuery] = `
SELECT header 
FROM sda.files 
WHERE id = $1;
`
}

func (db *pgDb) getHeader(ctx context.Context, tx *sql.Tx, fileID string) ([]byte, error) {
	stmt := db.getPreparedStmt(tx, getHeaderQuery)

	var hexString string
	if err := stmt.QueryRowContext(ctx, fileID).Scan(&hexString); err != nil {
		return nil, err
	}

	header, err := hex.DecodeString(hexString)
	if err != nil {
		return nil, err
	}

	return header, nil
}
