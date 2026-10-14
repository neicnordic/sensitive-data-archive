package postgres

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

const (
	listDatasetFilesQuery = "listDatasetFiles"
)

func init() {
	queries[listDatasetFilesQuery] = `SELECT f.id, f.stable_id,
       COALESCE(fd.download_path, f.submission_file_path) AS path,
       f.archive_file_size, f.decrypted_file_size, f.created_at,
       COALESCE(cs.checksums, '[]'::json) AS checksums,
       fd.id AS cursor_id
FROM sda.file_dataset fd
		JOIN sda.files f ON f.id = fd.file_id
		LEFT JOIN LATERAL (
			SELECT JSON_AGG(JSON_BUILD_OBJECT('type', lower(c.type::text), 'checksum', c.checksum)) AS checksums
			FROM sda.checksums c
			WHERE c.file_id = f.id AND c.source = 'UNENCRYPTED'
		) cs ON true
WHERE fd.dataset_id = (SELECT id FROM sda.datasets WHERE stable_id = $1)
	AND fd.id > $2
	AND f.stable_id IS NOT NULL
ORDER BY fd.id
LIMIT $3;`
}

func (db *pgDb) listDatasetFiles(ctx context.Context, tx *sql.Tx, datasetAccessionID string, limit int, cursor string) ([]*database.DatasetFileInfo, string, error) {
	// default limit: non-positive means unlimited (return all rows, no cursor emitted).
	// Clamped to math.MaxInt32-1 so that fetchLim = lim+1 never overflows int32
	lim := limit
	if lim <= 0 {
		lim = math.MaxInt32 - 1
	}
	// Fetch one extra row to determine whether a next page exists.
	fetchLim := lim + 1

	var cursorArg uint64
	if cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %v", database.ErrInvalidCursor, err)
		}
		if len(decoded) != 8 {
			return nil, "", fmt.Errorf("%w: decoded cursor must be 8 bytes", database.ErrInvalidCursor)
		}
		cursorArg = binary.LittleEndian.Uint64(decoded)
		if cursorArg > math.MaxInt32 {
			return nil, "", fmt.Errorf("%w: decoded cursor is out of range", database.ErrInvalidCursor)
		}
	}

	stmt, err := db.getPreparedStmt(tx, listDatasetFilesQuery)
	if err != nil {
		return nil, "", err
	}

	rows, err := stmt.QueryContext(ctx, datasetAccessionID, cursorArg, fetchLim)
	if err != nil {
		return nil, "", parsePQError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var files []*database.DatasetFileInfo
	// Iterate rows
	var nextFdID uint64
	for rows.Next() {
		dfi := new(database.DatasetFileInfo)
		var checksumsJSON []byte
		var fdID uint64
		// Scanning to NullInt64 for safety, we expect these columns to be populated for files in a dataset
		var archiveSize, decryptedSize sql.NullInt64

		if err := rows.Scan(&dfi.ID, &dfi.AccessionID, &dfi.Path, &archiveSize, &decryptedSize, &dfi.CreatedAt, &checksumsJSON, &fdID); err != nil {
			return nil, "", parsePQError(err)
		}

		dfi.Size = archiveSize.Int64
		dfi.DecryptedSize = decryptedSize.Int64

		if checksumsJSON != nil {
			if err := json.Unmarshal(checksumsJSON, &dfi.Checksums); err != nil {
				return nil, "", fmt.Errorf("failed to unmarshal checksums for file %s: %w", dfi.ID, err)
			}
		}

		files = append(files, dfi)

		// Track cursor position only up to lim rows; the (lim+1)-th row just
		// signals that more data exists and is not returned to the caller.
		if len(files) <= lim {
			nextFdID = fdID
		}
	}

	if err := rows.Err(); err != nil {
		return nil, "", parsePQError(err)
	}

	// Determine next cursor: present only when the extra probe row was returned.
	hasMore := len(files) > lim
	if hasMore {
		files = files[:lim]
	}
	nextCursor := ""
	if hasMore && nextFdID != 0 {
		// cursor is base64url("file_dataset.id")
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, nextFdID)

		nextCursor = base64.RawURLEncoding.EncodeToString(b)
	}

	return files, nextCursor, nil
}
