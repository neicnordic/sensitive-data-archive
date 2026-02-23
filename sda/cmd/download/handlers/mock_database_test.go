package handlers

import (
	"context"
	"sync"

	"github.com/neicnordic/sensitive-data-archive/cmd/download/audit"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/database"
)

// capturingLogger records audit events for test assertions.
type capturingLogger struct {
	events []audit.Event
	mu     sync.Mutex
}

func (l *capturingLogger) Log(_ context.Context, event audit.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *capturingLogger) IsNoop() bool { return false }

// mockDatabase is a mock implementation of the database.Database interface for testing.
type mockDatabase struct {
	datasets          []database.Dataset
	datasetIDs        []string
	datasetInfo       *database.DatasetInfo
	datasetFiles      []database.File
	datasetFilesPaged []database.File
	fileByID          *database.File
	fileByPath        *database.File
	hasPermission     bool
	datasetNotFound   bool
	err               error
	pingErr           error
}

func (m *mockDatabase) Ping(_ context.Context) error {
	return m.pingErr
}

func (m *mockDatabase) Close() error {
	return nil
}

func (m *mockDatabase) GetAllDatasets(_ context.Context) ([]database.Dataset, error) {
	if m.err != nil {
		return nil, m.err
	}

	return m.datasets, nil
}

func (m *mockDatabase) GetDatasetIDsByUser(_ context.Context, _ string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}

	return m.datasetIDs, nil
}

func (m *mockDatabase) GetUserDatasets(_ context.Context, _ []string) ([]database.Dataset, error) {
	if m.err != nil {
		return nil, m.err
	}

	return m.datasets, nil
}

func (m *mockDatabase) GetDatasetInfo(_ context.Context, _ string) (*database.DatasetInfo, error) {
	if m.err != nil {
		return nil, m.err
	}

	return m.datasetInfo, nil
}

func (m *mockDatabase) GetDatasetFiles(_ context.Context, _ string) ([]database.File, error) {
	if m.err != nil {
		return nil, m.err
	}

	return m.datasetFiles, nil
}

func (m *mockDatabase) GetFileByID(_ context.Context, _ string) (*database.File, error) {
	if m.err != nil {
		return nil, m.err
	}

	return m.fileByID, nil
}

func (m *mockDatabase) GetFileByPath(_ context.Context, _, _ string) (*database.File, error) {
	if m.err != nil {
		return nil, m.err
	}

	return m.fileByPath, nil
}

func (m *mockDatabase) CheckFilePermission(_ context.Context, _ string, _ []string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}

	return m.hasPermission, nil
}

func (m *mockDatabase) CheckDatasetExists(_ context.Context, _ string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}

	return !m.datasetNotFound, nil
}

func (m *mockDatabase) GetDatasetFilesPaginated(_ context.Context, _ string, _ database.FileListOptions) ([]database.File, error) {
	if m.err != nil {
		return nil, m.err
	}

	return m.datasetFilesPaged, nil
}
