package postgres

import (
	"context"
	"database/sql"
)

const checkAccessionIDExistsSameIDQuery = "checkAccessionIDExistsSameID"
const checkAccessionIDExistsQuery = "checkAccessionIDExists"

func init() {
	queries[checkAccessionIDExistsSameIDQuery] = `
SELECT COUNT(id) 
FROM sda.files 
WHERE stable_id = $1 
AND id = $2;
`
	queries[checkAccessionIDExistsQuery] = `
SELECT COUNT(id) 
FROM sda.files 
WHERE stable_id = $1;
`
}

func (db *pgDb) checkAccessionIDExists(ctx context.Context, tx *sql.Tx, accessionID, fileID string) (string, error) {
	sameIDStmt := db.getPreparedStmt(tx, checkAccessionIDExistsSameIDQuery)

	idExistsStmt := db.getPreparedStmt(tx, checkAccessionIDExistsQuery)

	var same int
	if err := sameIDStmt.QueryRowContext(ctx, accessionID, fileID).Scan(&same); err != nil {
		return "", err
	}

	if same > 0 {
		return "same", nil
	}

	var accessionIDCount int
	if err := idExistsStmt.QueryRowContext(ctx, accessionID).Scan(&accessionIDCount); err != nil {
		return "", err
	}

	if accessionIDCount > 0 {
		return "duplicate", nil
	}

	return "", nil
}
