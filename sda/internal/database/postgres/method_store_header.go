package postgres

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
)

const storeHeaderQuery = "storeHeader"

func init() {
	queries[storeHeaderQuery] = `
UPDATE sda.files 
SET header = $1 
WHERE id = $2;
`
}

func (db *pgDb) storeHeader(ctx context.Context, tx *sql.Tx, header []byte, id string) error {
	stmt := db.getPreparedStmt(tx, storeHeaderQuery)

	result, err := stmt.ExecContext(ctx, hex.EncodeToString(header), id)
	if err != nil {
		return err
	}
	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}
