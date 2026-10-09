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

const (
	getUserFilesQuery             = "getUserFiles"
	getUserFilesByPathPrefixQuery = "getUserFilesByPathPrefix"
)

func init() {
	// Without a path prefix the files are read in id order from
	// files_submission_user_id_idx, so a page stops after limit+1 rows.
	queries[getUserFilesQuery] = `SELECT f.id, f.submission_file_path, f.stable_id, COALESCE(f.last_event, '') as event, f.created_at, f.submission_file_size
FROM sda.files AS f
	LEFT JOIN sda.file_dataset AS fd ON fd.file_id = f.id
 WHERE f.submission_user = $1
	AND fd.file_id IS NULL AND COALESCE(f.last_event, '') NOT IN ('disabled', 'removed')
	AND ($2::UUID IS NULL OR f.id > $2::UUID)
ORDER BY f.id ASC LIMIT $3;`

	// The path prefix is matched as the byte range [prefix, prefix || U+10FFFF) in the C
	// collation, so it is served by files_submission_user_submission_file_path_c_idx instead
	// of filtering every file of the user.
	queries[getUserFilesByPathPrefixQuery] = `SELECT f.id, f.submission_file_path, f.stable_id, COALESCE(f.last_event, '') as event, f.created_at, f.submission_file_size
FROM sda.files AS f
	LEFT JOIN sda.file_dataset AS fd ON fd.file_id = f.id
 WHERE f.submission_user = $1
	AND f.submission_file_path COLLATE "C" >= $4::TEXT
	AND f.submission_file_path COLLATE "C" < $4::TEXT || chr(1114111)
	AND fd.file_id IS NULL AND COALESCE(f.last_event, '') NOT IN ('disabled', 'removed')
	AND ($2::UUID IS NULL OR f.id > $2::UUID)
ORDER BY f.id ASC LIMIT $3;`
}

func (db *pgDb) getUserFiles(ctx context.Context, tx *sql.Tx, userID, pathPrefix string, allData bool, limit int, cursor string) ([]*database.SubmissionFileInfo, string, error) {
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

	// The two queries are kept apart, instead of one query with an optional prefix
	// condition, so that the generic plan of each prepared statement uses its own index.
	queryName, args := getUserFilesQuery, []any{userID, cursorArg, fetchLim}
	if pathPrefix != "" {
		queryName, args = getUserFilesByPathPrefixQuery, append(args, pathPrefix)
	}
	stmt, err := db.getPreparedStmt(tx, queryName)
	if err != nil {
		return nil, "", err
	}

	rows, err := stmt.QueryContext(ctx, args...)
	if err != nil {
		return nil, "", parsePQError(err)
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

		if err := rows.Scan(&fi.FileID, &fi.InboxPath, &accessionID, &fi.Status, &fi.CreatedAt, &submissionFileSize); err != nil {
			return nil, "", parsePQError(err)
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
		return nil, "", parsePQError(err)
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
