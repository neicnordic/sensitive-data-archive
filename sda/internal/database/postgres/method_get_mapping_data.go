package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

const getMappingDataQuery = "getMappingData"

func init() {
	queries[getMappingDataQuery] = `
SELECT id, submission_user, submission_file_path, submission_location 
FROM sda.files 
WHERE stable_id = $1;
`
}

func (db *pgDb) getMappingData(ctx context.Context, tx *sql.Tx, accessionID string) (*database.MappingData, error) {
	stmt := db.getPreparedStmt(tx, getMappingDataQuery)

	data := &database.MappingData{}
	var submissionLocation sql.NullString
	if err := stmt.QueryRowContext(ctx, accessionID).Scan(&data.FileID, &data.User, &data.SubmissionFilePath, &submissionLocation); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}

		return nil, err
	}

	if submissionLocation.Valid {
		data.SubmissionLocation = submissionLocation.String
	}

	return data, nil
}
