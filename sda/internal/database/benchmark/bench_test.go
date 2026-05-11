package benchmark

import (
	"context"
	"fmt"
	"os"
	"testing"
)

func benchMode() string {
	if os.Getenv("BENCH_ADAPTER") == "noprep" {
		return "noprep"
	}
	return "prep"
}

func BenchmarkUpdateFileEventLog(b *testing.B) {
	truncateTables(b, testDB)
	fileID := seedFile(b, testDB)

	a := newAdapter(b, testDB, benchMode())
	defer a.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := a.exec(ctx, "updateFileEventLog",
			fileID, "uploaded", "bench-user", nil, nil); err != nil {
			b.Fatalf("exec: %v", err)
		}
	}
	b.StopTimer()
}

func BenchmarkGetFileStatus(b *testing.B) {
	truncateTables(b, testDB)
	fileID := seedFile(b, testDB)
	if _, err := testDB.Exec(
		`INSERT INTO sda.file_event_log(file_id, event, user_id, details, message)
		 VALUES($1, 'uploaded', 'bench-user', NULL, NULL)`, fileID); err != nil {
		b.Fatalf("seed event: %v", err)
	}

	a := newAdapter(b, testDB, benchMode())
	defer a.Close()
	ctx := context.Background()
	var event string

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := a.queryRow(ctx, "getFileStatus", []any{fileID}, &event); err != nil {
			b.Fatalf("queryRow: %v", err)
		}
	}
	b.StopTimer()
}

func BenchmarkStoreHeader(b *testing.B) {
	truncateTables(b, testDB)
	fileID := seedFile(b, testDB)

	a := newAdapter(b, testDB, benchMode())
	defer a.Close()
	ctx := context.Background()
	header := "deadbeef"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := a.exec(ctx, "storeHeader", header, fileID); err != nil {
			b.Fatalf("exec: %v", err)
		}
	}
	b.StopTimer()
}

func BenchmarkGetHeader(b *testing.B) {
	truncateTables(b, testDB)
	fileID := seedFile(b, testDB)

	a := newAdapter(b, testDB, benchMode())
	defer a.Close()
	ctx := context.Background()
	var header string

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := a.queryRow(ctx, "getHeader", []any{fileID}, &header); err != nil {
			b.Fatalf("queryRow: %v", err)
		}
	}
	b.StopTimer()
}

func BenchmarkGetArchivePathAndLocation(b *testing.B) {
	truncateTables(b, testDB)
	_ = seedFile(b, testDB)
	accessionID := fmt.Sprintf("accession-%d", seedCounter)

	a := newAdapter(b, testDB, benchMode())
	defer a.Close()
	ctx := context.Background()
	var archivePath, archiveLoc string

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := a.queryRow(ctx, "getArchivePathAndLocation",
			[]any{accessionID}, &archivePath, &archiveLoc); err != nil {
			b.Fatalf("queryRow: %v", err)
		}
	}
	b.StopTimer()
}
