package postgres

import (
	"context"
	"database/sql"
)

const getDatasetFilesQuery = "getDatasetFiles"

func init() {
	queries[getDatasetFilesQuery] = `
SELECT stable_id 
FROM sda.files 
WHERE id IN (
	SELECT file_id 
	FROM sda.file_dataset 
	WHERE dataset_id = (
		SELECT id 
		FROM sda.datasets 
		WHERE stable_id = $1
		)
	);
`
}

func (db *pgDb) getDatasetFiles(ctx context.Context, tx *sql.Tx, datasetID string) ([]string, error) {
	stmt := db.getPreparedStmt(tx, getDatasetFilesQuery)

	var accessions []string
	rows, err := stmt.QueryContext(ctx, datasetID)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var accession string
		err := rows.Scan(&accession)
		if err != nil {
			return nil, err
		}

		accessions = append(accessions, accession)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return accessions, nil
}
