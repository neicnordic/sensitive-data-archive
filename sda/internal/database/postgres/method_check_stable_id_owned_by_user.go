package postgres

import (
	"context"
	"database/sql"
	"errors"
)

const checkAccessionIDOwnedByUserQuery = "checkAccessionIDOwnedByUser"

func init() {
	queries[checkAccessionIDOwnedByUserQuery] = `
SELECT true
FROM sda.files
WHERE stable_id = $1
AND submission_user = $2;
`
}

func (db *pgDb) checkAccessionIDOwnedByUser(ctx context.Context, tx *sql.Tx, accessionID, user string) (bool, error) {
	stmt := db.getPreparedStmt(tx, checkAccessionIDOwnedByUserQuery)

	var found bool
	if err := stmt.QueryRowContext(ctx, accessionID, user).Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}
