package postgres

import (
	"context"
	"database/sql"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

const getUserFilesQuery = "getUserFiles"

func init() {
	queries[getUserFilesQuery] = `
SELECT DISTINCT ON (f.id) f.id, f.submission_file_path, f.stable_id, fel.event, f.created_at, f.submission_file_size FROM sda.files AS f
    LEFT JOIN sda.file_event_log AS fel ON fel.file_id = f.id
    LEFT JOIN sda.file_dataset AS fd ON fd.file_id = f.id
WHERE f.submission_user = $1 
	AND ($2::TEXT IS NULL OR substr(f.submission_file_path, 1, $3) = $2::TEXT)
    AND fd.file_id IS NULL
ORDER BY f.id, fel.started_at DESC;
`
}

func (db *pgDb) getUserFiles(ctx context.Context, tx *sql.Tx, userID, pathPrefix string, allData bool) ([]*database.SubmissionFileInfo, error) {
	stmt := db.getPreparedStmt(tx, getUserFilesQuery)

	pathPrefixLen := 1
	pathPrefixArg := sql.NullString{}
	if pathPrefix != "" {
		pathPrefixLen = len(pathPrefix)
		pathPrefixArg.Valid = true
		pathPrefixArg.String = pathPrefix
	}

	rows, err := stmt.QueryContext(ctx, userID, pathPrefixArg, pathPrefixLen)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var files []*database.SubmissionFileInfo
	// Iterate rows
	for rows.Next() {
		var accessionID sql.NullString
		// Read rows into struct
		fi := new(database.SubmissionFileInfo)
		var submissionFileSize sql.NullInt64
		err := rows.Scan(&fi.FileID, &fi.InboxPath, &accessionID, &fi.Status, &fi.CreatedAt, &submissionFileSize)
		if err != nil {
			return nil, err
		}

		if submissionFileSize.Valid {
			fi.SubmissionFileSize = submissionFileSize.Int64
		}

		if allData {
			fi.AccessionID = accessionID.String
		}

		// Add instance of struct (file) to array if the status is not disabled
		if fi.Status != "disabled" {
			files = append(files, fi)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return files, nil
}
