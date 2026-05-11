package benchmark

// queries holds SQL copied verbatim from Karl's postgres package on SHA 5b09a151.
// Source paths per entry; verified by `TestBenchVerifyCopiedSQL` / `make bench-verify`.
//
// NOTE: trailing whitespace on multi-line entries is intentional — Karl's source
// has spaces before newlines on the SELECT/FROM/WHERE/ORDER BY lines, and the
// drift check is a verbatim byte-compare.
var queries = map[string]string{
	// sda/internal/database/postgres/method_update_file_event_log.go:14
	"updateFileEventLog": `
INSERT INTO sda.file_event_log(file_id, event, user_id, details, message)
VALUES($1, $2, $3, $4, $5);
`,

	// sda/internal/database/postgres/method_get_file_status.go:11
	"getFileStatus": `
SELECT event
FROM sda.file_event_log
WHERE file_id = $1
ORDER BY id DESC LIMIT 1;
`,

	// sda/internal/database/postgres/method_store_header.go:12
	"storeHeader": `
UPDATE sda.files
SET header = $1
WHERE id = $2;
`,

	// sda/internal/database/postgres/method_get_header.go:11
	"getHeader": `
SELECT header
FROM sda.files
WHERE id = $1;
`,

	// sda/internal/database/postgres/method_get_archive_path_and_location.go:11
	"getArchivePathAndLocation": `
SELECT archive_file_path, archive_location
FROM sda.files
WHERE stable_id = $1;
`,
}
