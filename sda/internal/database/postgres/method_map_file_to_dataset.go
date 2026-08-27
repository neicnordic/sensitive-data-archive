package postgres

import (
	"context"
	"database/sql"
)

const mapFileToDatasetQuery = "mapFileToDataset"
const mapFileToDatasetInsertDatasetQuery = "mapFileToDatasetInsertDataset"

func init() {
	queries[mapFileToDatasetQuery] = `INSERT INTO sda.file_dataset (file_id, dataset_id, download_path)
VALUES ($1, $2, $3) ON CONFLICT ON CONSTRAINT unique_file_dataset DO NOTHING;`

	// Here we do the UPDATE SET stable_id = EXCLUDED.stable_id to make the RETURNING id return the id
	// with a ON CONFLICT DO NOTHING, the RETURNING id will not return the ID
	// This is to reduce the need for an additional SELECT query after the insert
	queries[mapFileToDatasetInsertDatasetQuery] = `INSERT INTO sda.datasets (stable_id) 
VALUES ($1) 
ON CONFLICT (stable_id) DO 
	UPDATE SET stable_id = EXCLUDED.stable_id
RETURNING id;`
}

func (db *pgDb) mapFileToDataset(ctx context.Context, tx *sql.Tx, datasetID, fileID string, downloadPath *string) error {
	mapFileToDatasetStmt, err := db.getPreparedStmt(tx, mapFileToDatasetQuery)
	if err != nil {
		return err
	}

	insertDatasetStmt, err := db.getPreparedStmt(tx, mapFileToDatasetInsertDatasetQuery)
	if err != nil {
		return err
	}

	var dbDatasetID string

	if err := insertDatasetStmt.QueryRowContext(ctx, datasetID).Scan(&dbDatasetID); err != nil {
		return parsePQError(err)
	}

	if _, err := mapFileToDatasetStmt.ExecContext(ctx, fileID, dbDatasetID, downloadPath); err != nil {
		return parsePQError(err)
	}

	return nil
}
