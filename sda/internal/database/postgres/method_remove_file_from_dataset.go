package postgres

import (
	"context"
	"database/sql"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

const removeFileFromDatasetQuery = "removeFileFromDataset"

func init() {
	queries[removeFileFromDatasetQuery] = `DELETE FROM sda.file_dataset
WHERE dataset_id = (SELECT id FROM sda.datasets WHERE stable_id = $1) 
AND file_id = (SELECT id FROM sda.files WHERE stable_id = $2)`
}

func (db *pgDb) removeFileFromDataset(ctx context.Context, tx *sql.Tx, datasetID, accessionID string) error {
	stmt, err := db.getPreparedStmt(tx, removeFileFromDatasetQuery)
	if err != nil {
		return err
	}

	result, err := stmt.ExecContext(ctx, datasetID, accessionID)
	if err != nil {
		return parsePQError(err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return parsePQError(err)
	}

	if rowsAffected == 0 {
		return database.ErrNoRowsAffected
	}

	return nil
}
