package postgres

import (
	"context"
	"database/sql"
	"errors"
)

const updateDatasetEventQuery = "updateDatasetEvent"

func init() {
	queries[updateDatasetEventQuery] = `
INSERT INTO sda.dataset_event_log(dataset_id, event, message) 
VALUES($1, $2, $3);
`
}
func (db *pgDb) updateDatasetEvent(ctx context.Context, tx *sql.Tx, datasetID, status, message string) error {
	stmt := db.getPreparedStmt(tx, updateDatasetEventQuery)

	result, err := stmt.ExecContext(ctx, datasetID, status, message)
	if err != nil {
		return err
	}

	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}
