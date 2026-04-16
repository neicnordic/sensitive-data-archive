package postgres

import (
	"context"
	"database/sql"
	"errors"
)

const addKeyHashQuery = "addKeyHash"

func init() {
	queries[addKeyHashQuery] = `
INSERT INTO sda.encryption_keys(key_hash, description) 
VALUES($1, $2) ON CONFLICT DO NOTHING;
`
}

func (db *pgDb) addKeyHash(ctx context.Context, tx *sql.Tx, keyHash, keyDescription string) error {
	stmt := db.getPreparedStmt(tx, addKeyHashQuery)

	result, err := stmt.ExecContext(ctx, keyHash, keyDescription)
	if err != nil {
		return err
	}

	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("key hash already exists or no rows were updated")
	}

	return nil
}
