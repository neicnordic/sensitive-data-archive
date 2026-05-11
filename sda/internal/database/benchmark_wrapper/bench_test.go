package benchmark_wrapper

import (
	"context"
	"fmt"
	"testing"
)

// Variant 01c — karl-wrapper. Each bench mirrors its 01b counterpart
// (same parent name, same fixture shape) but dispatches through Karl's
// public database.Database interface. Comparing 01c to 01b isolates any
// overhead Karl's wrapper adds (interface dispatch, getPreparedStmt
// map lookup, the tx=nil branch) from the raw `stmt.ExecContext` path.

// storeHeaderPayload is the []byte whose hex encoding equals "deadbeef",
// the payload 01b writes directly as a hex string. Karl's StoreHeader
// hex-encodes its argument internally, so this reproduces the same on-wire
// UPDATE as 01b.
var storeHeaderPayload = []byte{0xde, 0xad, 0xbe, 0xef}

func BenchmarkUpdateFileEventLog(b *testing.B) {
	truncateTables(b, rawDB)
	fileID := seedFile(b, rawDB)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// "{}" (empty JSON object) matches Karl's real callers in cmd/{ingest,verify,finalize,s3inbox}/.
		// The adapter bench passes nil→NULL for these JSONB columns; this wrapper bench passes "{}"
		// which parses as an empty object. The extra JSONB parse is nanoseconds and, if anything,
		// biases this run slightly slower than the adapter bench — it cannot mask wrapper overhead.
		if err := karlDB.UpdateFileEventLog(ctx, fileID, "uploaded", "bench-user", "{}", "{}"); err != nil {
			b.Fatalf("UpdateFileEventLog: %v", err)
		}
	}
	b.StopTimer()
}

func BenchmarkGetFileStatus(b *testing.B) {
	truncateTables(b, rawDB)
	fileID := seedFile(b, rawDB)
	// 01b seeds one file_event_log row so GetFileStatus has a hit.
	if _, err := rawDB.Exec(
		`INSERT INTO sda.file_event_log(file_id, event, user_id, details, message)
		 VALUES($1, 'uploaded', 'bench-user', NULL, NULL)`, fileID); err != nil {
		b.Fatalf("seed event: %v", err)
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := karlDB.GetFileStatus(ctx, fileID); err != nil {
			b.Fatalf("GetFileStatus: %v", err)
		}
	}
	b.StopTimer()
}

func BenchmarkStoreHeader(b *testing.B) {
	truncateTables(b, rawDB)
	fileID := seedFile(b, rawDB)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := karlDB.StoreHeader(ctx, storeHeaderPayload, fileID); err != nil {
			b.Fatalf("StoreHeader: %v", err)
		}
	}
	b.StopTimer()
}

func BenchmarkGetHeader(b *testing.B) {
	truncateTables(b, rawDB)
	fileID := seedFile(b, rawDB)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := karlDB.GetHeader(ctx, fileID); err != nil {
			b.Fatalf("GetHeader: %v", err)
		}
	}
	b.StopTimer()
}

func BenchmarkGetArchivePathAndLocation(b *testing.B) {
	truncateTables(b, rawDB)
	_ = seedFile(b, rawDB)
	accessionID := fmt.Sprintf("accession-%d", seedCounter)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := karlDB.GetArchivePathAndLocation(ctx, accessionID); err != nil {
			b.Fatalf("GetArchivePathAndLocation: %v", err)
		}
	}
	b.StopTimer()
}
