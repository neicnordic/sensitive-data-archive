package postgres

import (
	"context"
	"database/sql"
)

const mapFileToDatasetQuery = "mapFileToDataset"
const mapFileToDatasetInsertDatasetQuery = "mapFileToDatasetInsertDataset"

func init() {
	queries[mapFileToDatasetQuery] = `
INSERT INTO sda.file_dataset (file_id, dataset_id)
VALUES ($1, $2) ON CONFLICT DO NOTHING;
`

	// Here we do the UPDATE SET stable_id = EXCLUDED.stable_id to make the RETURNING id return the id
	// with a ON CONFLICT DO NOTHING, the RETURNING id will not return the ID
	// This is to reduce the need for an additional SELECT query after the insert
	queries[mapFileToDatasetInsertDatasetQuery] = `
INSERT INTO sda.datasets (stable_id) 
VALUES ($1) 
ON CONFLICT (stable_id) DO 
	UPDATE SET stable_id = EXCLUDED.stable_id
RETURNING id;
`
}

func (db *pgDb) mapFileToDataset(ctx context.Context, tx *sql.Tx, datasetID, fileID string) error {
	stmt := db.getPreparedStmt(tx, mapFileToDatasetQuery)

	insertDatasetStmt := db.getPreparedStmt(tx, mapFileToDatasetInsertDatasetQuery)

	var dbDatasetID string

	if err := insertDatasetStmt.QueryRowContext(ctx, datasetID).Scan(&dbDatasetID); err != nil {
		return err
	}

	if _, err := stmt.ExecContext(ctx, fileID, dbDatasetID); err != nil {
		return err
	}

	return nil
}
