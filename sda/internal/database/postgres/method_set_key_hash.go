package postgres

import (
	"context"
	"database/sql"
	"errors"
)

const setKeyHashQuery = "setKeyHash"

func init() {
	queries[setKeyHashQuery] = `
UPDATE sda.files 
SET key_hash = $1 
WHERE id = $2;
`
}
func (db *pgDb) setKeyHash(ctx context.Context, tx *sql.Tx, keyHash, fileID string) error {
	stmt := db.getPreparedStmt(tx, setKeyHashQuery)

	result, err := stmt.ExecContext(ctx, keyHash, fileID)
	if err != nil {
		return err
	}
	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query, zero rows were changed")
	}

	return nil
}
