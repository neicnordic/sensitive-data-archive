package postgres

import (
	"context"
	"database/sql"
	"errors"
)

const setAccessionIDQuery = "setAccessionID"

func init() {
	queries[setAccessionIDQuery] = `
UPDATE sda.files 
SET stable_id = $1 
WHERE id = $2;
`
}

func (db *pgDb) setAccessionID(ctx context.Context, tx *sql.Tx, accessionID, fileID string) error {
	stmt := db.getPreparedStmt(tx, setAccessionIDQuery)

	result, err := stmt.ExecContext(ctx, accessionID, fileID)
	if err != nil {
		return err
	}

	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}
