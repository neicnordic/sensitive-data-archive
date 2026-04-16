package postgres

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

const getUserFilesQuery = "getUserFiles"

func init() {
	queries[getUserFilesQuery] = `
SELECT f.id, f.submission_file_path, f.stable_id, COALESCE(f.last_event, '') as event, f.created_at, f.submission_file_size
FROM sda.files AS f
	LEFT JOIN sda.file_dataset AS fd ON fd.file_id = f.id
 WHERE f.submission_user = $1 AND ($2::TEXT IS NULL OR substr(f.submission_file_path, 1, $3) = $2::TEXT)
	AND fd.file_id IS NULL AND COALESCE(f.last_event, '') != 'disabled'
	AND ($4::UUID IS NULL OR f.id > $4::UUID)
ORDER BY f.id ASC LIMIT $5;
`
}

func (db *pgDb) getUserFiles(ctx context.Context, tx *sql.Tx, userID, pathPrefix string, allData bool, limit int, cursor string) ([]*database.SubmissionFileInfo, string, error) {
	stmt := db.getPreparedStmt(tx, getUserFilesQuery)

	pathPrefixLen := 1
	pathPrefixArg := sql.NullString{}
	if pathPrefix != "" {
		pathPrefixLen = len(pathPrefix)
		pathPrefixArg.Valid = true
		pathPrefixArg.String = pathPrefix
	}

	// default limit: 0 means unlimited (return all rows, no cursor emitted).
	// Clamped to math.MaxInt32-1 so that fetchLim = lim+1 never overflows int32
	// on 32-bit platforms and avoids sending math.MaxInt32+1 as a LIMIT to Postgres.
	lim := limit
	if lim <= 0 {
		lim = math.MaxInt32 - 1
	}
	// Fetch one extra row to determine whether a next page exists.
	fetchLim := lim + 1

	cursorArg := sql.NullString{}
	if cursor != "" {
		decoded, derr := base64.RawURLEncoding.DecodeString(cursor)
		if derr != nil {
			return nil, "", fmt.Errorf("%w: %v", database.ErrInvalidCursor, derr)
		}
		decodedStr := string(decoded)
		if _, parseErr := uuid.Parse(decodedStr); parseErr != nil {
			return nil, "", fmt.Errorf("%w: decoded cursor is not a valid file ID", database.ErrInvalidCursor)
		}
		cursorArg.Valid = true
		cursorArg.String = decodedStr
	}

	rows, err := stmt.QueryContext(ctx, userID, pathPrefixArg, pathPrefixLen, cursorArg, fetchLim)
	if err != nil {
		return nil, "", err
	}
	defer func() {
		_ = rows.Close()
	}()

	var files []*database.SubmissionFileInfo
	// Iterate rows
	var lastID string
	for rows.Next() {
		var accessionID sql.NullString
		// Read rows into struct
		fi := new(database.SubmissionFileInfo)
		var submissionFileSize sql.NullInt64
		err := rows.Scan(&fi.FileID, &fi.InboxPath, &accessionID, &fi.Status, &fi.CreateAt, &submissionFileSize)
		if err != nil {
			return nil, "", err
		}

		if submissionFileSize.Valid {
			fi.SubmissionFileSize = submissionFileSize.Int64
		}

		if allData {
			fi.AccessionID = accessionID.String
		}

		files = append(files, fi)

		// Track cursor position only up to lim rows; the (lim+1)-th row just
		// signals that more data exists and is not returned to the caller.
		if len(files) <= lim {
			lastID = fi.FileID
		}
	}

	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	// Determine next cursor: present only when the extra probe row was returned.
	nextCursor := ""
	hasMore := len(files) > lim
	if hasMore {
		files = files[:lim]
	}
	if hasMore && lastID != "" {
		// cursor is base64url("<fileid>")
		nextCursor = base64.RawURLEncoding.EncodeToString([]byte(lastID))
	}

	return files, nextCursor, nil
}
