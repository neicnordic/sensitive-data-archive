package postgres

import (
	"context"
	"database/sql"
	"errors"
)

const getSubmissionLocationQuery = "getSubmissionLocation"

func init() {
	queries[getSubmissionLocationQuery] = `
SELECT submission_location 
FROM sda.files 
WHERE id = $1;
`
}

func (db *pgDb) getSubmissionLocation(ctx context.Context, tx *sql.Tx, fileID string) (string, error) {
	stmt := db.getPreparedStmt(tx, getSubmissionLocationQuery)

	var submissionLocation string
	if err := stmt.QueryRowContext(ctx, fileID).Scan(&submissionLocation); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}

		return "", err
	}

	return submissionLocation, nil
}
