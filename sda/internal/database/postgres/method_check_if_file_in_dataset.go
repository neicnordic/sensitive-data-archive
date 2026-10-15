package postgres

import (
	"context"
	"database/sql"
)

const checkIfFileInDatasetQuery = "checkIfFileInDataset"

func init() {
	queries[checkIfFileInDatasetQuery] = `SELECT EXISTS(
SELECT f.id 
FROM sda.datasets AS d
INNER JOIN sda.file_dataset AS fd ON fd.dataset_id = d.id
INNER JOIN sda.files AS f ON f.id = fd.file_id
WHERE d.stable_id = $1
AND f.stable_id = $2
);`
}

func (db *pgDb) checkIfFileInDataset(ctx context.Context, tx *sql.Tx, datasetID, accessionID string) (bool, error) {
	stmt, err := db.getPreparedStmt(tx, checkIfFileInDatasetQuery)
	if err != nil {
		return false, err
	}

	var yesNo bool
	if err := stmt.QueryRowContext(ctx, datasetID, accessionID).Scan(&yesNo); err != nil {
		return yesNo, parsePQError(err)
	}

	return yesNo, nil
}
