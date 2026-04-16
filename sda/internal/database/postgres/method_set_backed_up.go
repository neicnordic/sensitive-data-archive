package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

const setBackedUpQuery = "setBackedUp"

func init() {
	queries[setBackedUpQuery] = `
UPDATE sda.files 
SET backup_location = $1, backup_path = $2 
WHERE id = $3;
`
}

func (db *pgDb) setBackedUp(ctx context.Context, tx *sql.Tx, location, path, fileID string) error {
	stmt := db.getPreparedStmt(tx, setBackedUpQuery)

	r, err := stmt.ExecContext(ctx, location, path, fileID)
	if err != nil {
		return fmt.Errorf("setBackedUp error: %s", err.Error())
	}

	rowsAffected, err := r.RowsAffected()
	if err != nil {
		return fmt.Errorf("setBackedUp error: %s", err.Error())
	}

	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}
