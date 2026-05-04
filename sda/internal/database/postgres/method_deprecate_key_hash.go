package postgres

import (
	"context"
	"database/sql"
	"errors"
)

const deprecateKeyHashQuery = "deprecateKeyHash"

func init() {
	queries[deprecateKeyHashQuery] = `
UPDATE sda.encryption_keys 
SET deprecated_at = NOW() 
WHERE key_hash = $1 
AND deprecated_at IS NULL;
`
}

func (db *pgDb) deprecateKeyHash(ctx context.Context, tx *sql.Tx, keyHash string) error {
	stmt := db.getPreparedStmt(tx, deprecateKeyHashQuery)

	result, err := stmt.ExecContext(ctx, keyHash)
	if err != nil {
		return err
	}

	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("key hash not found or already deprecated")
	}

	return nil
}
