package handlers

import (
	"context"

	"github.com/neicnordic/sensitive-data-archive/cmd/download/database"
)

// mockDatabase is a mock implementation of the database.Database interface for testing.
type mockDatabase struct {
	datasets      []database.Dataset
	datasetInfo   *database.DatasetInfo
	datasetFiles  []database.File
	fileByID      *database.File
	fileByPath    *database.File
	hasPermission bool
	err           error
}

func (m *mockDatabase) Close() error {
	return nil
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
