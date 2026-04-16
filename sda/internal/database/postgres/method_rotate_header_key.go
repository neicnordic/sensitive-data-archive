package postgres

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
)

const rotateHeaderKeyQuery = "rotateHeaderKey"

func init() {
	queries[rotateHeaderKeyQuery] = `
UPDATE sda.files 
SET header = $1, key_hash = $2 
WHERE id = $3;
`
}

func (db *pgDb) rotateHeaderKey(ctx context.Context, tx *sql.Tx, header []byte, keyHash, fileID string) error {
	stmt := db.getPreparedStmt(tx, rotateHeaderKeyQuery)

	result, err := stmt.ExecContext(ctx, hex.EncodeToString(header), keyHash, fileID)
	if err != nil {
		return err
	}
	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}
