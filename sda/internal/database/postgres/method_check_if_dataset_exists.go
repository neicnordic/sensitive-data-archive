package postgres

import (
	"context"
	"database/sql"
)

const checkIfDatasetExistsQuery = "checkIfDatasetExists"

func init() {
	queries[checkIfDatasetExistsQuery] = `
SELECT EXISTS(
	SELECT id from sda.datasets 
	WHERE stable_id = $1
);
`
}

func (db *pgDb) checkIfDatasetExists(ctx context.Context, tx *sql.Tx, datasetID string) (bool, error) {
	stmt := db.getPreparedStmt(tx, checkIfDatasetExistsQuery)

	var yesNo bool
	if err := stmt.QueryRowContext(ctx, datasetID).Scan(&yesNo); err != nil {
		return yesNo, err
	}

	return yesNo, nil
}
