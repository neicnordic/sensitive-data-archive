package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	brokerv2 "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestMapper(t *testing.T) {
	for _, tc := range []testCase{
		mappingSuccessCase,
		mappingSuccessWithInboxConfigCase,
		releaseSuccessCase,
		deprecatedSuccessCase,
		mappingSuccessWithFileDownloadPathsCase,
		unknownOperationCase,
		mappingSuccessNoSubmissionLocationsCase,
		mappingMapFileToDatasetRetryableErrorCase,
		mappingGetMappingDataRetryableErrorCase,
		mappingNoMappingDataFoundCase,
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockWriter, mockDatabase, mockBroker := tc.newMocks(t)
			v := &mapper{
				db:          mockDatabase,
				broker:      mockBroker,
				inboxWriter: mockWriter,
				inboxConfig: tc.inboxConfig,
				schemaPath:  "../../schemas/isolated/",
			}

			jsonMsg, err := json.Marshal(tc.sourceMessage)
			if err != nil {
				t.Errorf("failed to marshal source message: %s", err.Error())
			}
			callbacks, err := v.handleMessage(context.Background(), &brokerv2.Message{Key: tc.sourceMessage.DatasetID, Body: jsonMsg})
			for _, cb := range callbacks {
				cb()
			}
			assert.Equal(t, tc.expectedError, err)
			tc.assertMocks(t, mockWriter, mockDatabase, mockBroker)
		})
	}
}

type testCase struct {
	name          string
	sourceMessage schema.DatasetMapping
	newMocks      func(t *testing.T) (*mocks.MockWriter, *mocks.MockDatabase, *mocks.MockBroker)
	assertMocks   func(*testing.T, *mocks.MockWriter, *mocks.MockDatabase, *mocks.MockBroker)
	inboxConfig   helper.InboxProjectConfig
	expectedError error
}

var mappingSuccessCase = testCase{
	name: "mapping_success",
	sourceMessage: schema.DatasetMapping{
		Type:              "mapping",
		DatasetID:         "dataset_123",
		AccessionIDs:      []string{"file_accession_1", "file_accession_2", "file_accession_3", "file_accession_4", "file_accession_5"},
		FileDownloadPaths: nil,
	},
	newMocks: func(t *testing.T) (*mocks.MockWriter, *mocks.MockDatabase, *mocks.MockBroker) {
		mockWriter := &mocks.MockWriter{}
		mockDatabase := &mocks.MockDatabase{}

		mockDatabase.On("BeginTransaction").Return(nil).Once()
		mockDatabase.On("Rollback").Return(nil).Once()
		mockDatabase.On("Commit").Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_1").Return(&database.MappingData{
			FileID:             "file_id_1",
			User:               "test@user",
			SubmissionFilePath: "/file_path_1/file.c4gh",
			SubmissionLocation: "unit_test_submission_location",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_1", (*string)(nil)).Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_2").Return(&database.MappingData{
			FileID:             "file_id_2",
			User:               "test@user",
			SubmissionFilePath: "/file_path_2/file.c4gh",
			SubmissionLocation: "unit_test_submission_location",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_2", (*string)(nil)).Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_3").Return(&database.MappingData{
			FileID:             "file_id_3",
			User:               "test@user",
			SubmissionFilePath: "/file_path_3/file.c4gh",
			SubmissionLocation: "unit_test_submission_location",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_3", (*string)(nil)).Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_4").Return(&database.MappingData{
			FileID:             "file_id_4",
			User:               "test@user",
			SubmissionFilePath: "/file_path_4/file.c4gh",
			SubmissionLocation: "unit_test_submission_location_other",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_4", (*string)(nil)).Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_5").Return(&database.MappingData{
			FileID:             "file_id_5",
			User:               "test@user",
			SubmissionFilePath: "/file_path_5/file.c4gh",
			SubmissionLocation: "unit_test_submission_location",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_5", (*string)(nil)).Return(nil).Once()

		mockDatabase.On("UpdateDatasetEvent", "dataset_123", "registered", mock.Anything).Return(nil).Once()

		mockWriter.On("RemoveFile", "unit_test_submission_location", "test_user/file_path_1/file.c4gh").Return(nil).Once()
		mockWriter.On("RemoveFile", "unit_test_submission_location", "test_user/file_path_2/file.c4gh").Return(nil).Once()
		mockWriter.On("RemoveFile", "unit_test_submission_location", "test_user/file_path_3/file.c4gh").Return(nil).Once()
		mockWriter.On("RemoveFile", "unit_test_submission_location_other", "test_user/file_path_4/file.c4gh").Return(nil).Once()
		mockWriter.On("RemoveFile", "unit_test_submission_location", "test_user/file_path_5/file.c4gh").Return(nil).Once()

		return mockWriter, mockDatabase, &mocks.MockBroker{}
	},
	assertMocks: func(t *testing.T, mockReader *mocks.MockWriter, mockDatabase *mocks.MockDatabase, mockBroker *mocks.MockBroker) {
		mockReader.AssertExpectations(t)
		mockDatabase.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	},
	expectedError: nil,
}

var mappingSuccessWithInboxConfigCase = testCase{
	name: "mapping_success_with_inbox_config",
	sourceMessage: schema.DatasetMapping{
		Type:              "mapping",
		DatasetID:         "dataset_123",
		AccessionIDs:      []string{"file_accession_1", "file_accession_2", "file_accession_3", "file_accession_4", "file_accession_5"},
		FileDownloadPaths: nil,
	},
	inboxConfig: helper.InboxProjectConfig{
		Code:      "unit-test-project",
		Delimiter: ".",
	},
	newMocks: func(t *testing.T) (*mocks.MockWriter, *mocks.MockDatabase, *mocks.MockBroker) {
		mockWriter := &mocks.MockWriter{}
		mockDatabase := &mocks.MockDatabase{}

		mockDatabase.On("BeginTransaction").Return(nil).Once()
		mockDatabase.On("Rollback").Return(nil).Once()
		mockDatabase.On("Commit").Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_1").Return(&database.MappingData{
			FileID:             "file_id_1",
			User:               "test@user",
			SubmissionFilePath: "/file_path_1/file.c4gh",
			SubmissionLocation: "unit_test_submission_location",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_1", (*string)(nil)).Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_2").Return(&database.MappingData{
			FileID:             "file_id_2",
			User:               "test@user",
			SubmissionFilePath: "/file_path_2/file.c4gh",
			SubmissionLocation: "unit_test_submission_location",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_2", (*string)(nil)).Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_3").Return(&database.MappingData{
			FileID:             "file_id_3",
			User:               "test@user",
			SubmissionFilePath: "/file_path_3/file.c4gh",
			SubmissionLocation: "unit_test_submission_location",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_3", (*string)(nil)).Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_4").Return(&database.MappingData{
			FileID:             "file_id_4",
			User:               "test@user",
			SubmissionFilePath: "/file_path_4/file.c4gh",
			SubmissionLocation: "unit_test_submission_location_other",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_4", (*string)(nil)).Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_5").Return(&database.MappingData{
			FileID:             "file_id_5",
			User:               "test@user",
			SubmissionFilePath: "/file_path_5/file.c4gh",
			SubmissionLocation: "unit_test_submission_location",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_5", (*string)(nil)).Return(nil).Once()

		mockDatabase.On("UpdateDatasetEvent", "dataset_123", "registered", mock.Anything).Return(nil).Once()

		mockWriter.On("RemoveFile", "unit_test_submission_location", "unit-test-project.test@user/file_path_1/file.c4gh").Return(nil).Once()
		mockWriter.On("RemoveFile", "unit_test_submission_location", "unit-test-project.test@user/file_path_2/file.c4gh").Return(nil).Once()
		mockWriter.On("RemoveFile", "unit_test_submission_location", "unit-test-project.test@user/file_path_3/file.c4gh").Return(nil).Once()
		mockWriter.On("RemoveFile", "unit_test_submission_location_other", "unit-test-project.test@user/file_path_4/file.c4gh").Return(nil).Once()
		mockWriter.On("RemoveFile", "unit_test_submission_location", "unit-test-project.test@user/file_path_5/file.c4gh").Return(nil).Once()

		return mockWriter, mockDatabase, &mocks.MockBroker{}
	},
	assertMocks: func(t *testing.T, mockReader *mocks.MockWriter, mockDatabase *mocks.MockDatabase, mockBroker *mocks.MockBroker) {
		mockReader.AssertExpectations(t)
		mockDatabase.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	},
	expectedError: nil,
}

var releaseSuccessCase = testCase{
	name: "release_success",
	sourceMessage: schema.DatasetMapping{
		Type:      "release",
		DatasetID: "dataset_123",
	},
	newMocks: func(t *testing.T) (*mocks.MockWriter, *mocks.MockDatabase, *mocks.MockBroker) {
		mockDatabase := &mocks.MockDatabase{}

		mockDatabase.On("BeginTransaction").Return(nil).Once()
		mockDatabase.On("Rollback").Return(nil).Once()
		mockDatabase.On("Commit").Return(nil).Once()

		mockDatabase.On("UpdateDatasetEvent", "dataset_123", "released", mock.Anything).Return(nil).Once()

		return &mocks.MockWriter{}, mockDatabase, &mocks.MockBroker{}
	},
	assertMocks: func(t *testing.T, mockReader *mocks.MockWriter, mockDatabase *mocks.MockDatabase, mockBroker *mocks.MockBroker) {
		mockReader.AssertExpectations(t)
		mockDatabase.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	},
	expectedError: nil,
}

var deprecatedSuccessCase = testCase{
	name: "deprecated_success",
	sourceMessage: schema.DatasetMapping{
		Type:      "deprecate",
		DatasetID: "dataset_123",
	},
	newMocks: func(t *testing.T) (*mocks.MockWriter, *mocks.MockDatabase, *mocks.MockBroker) {
		mockDatabase := &mocks.MockDatabase{}

		mockDatabase.On("BeginTransaction").Return(nil).Once()
		mockDatabase.On("Rollback").Return(nil).Once()
		mockDatabase.On("Commit").Return(nil).Once()

		mockDatabase.On("UpdateDatasetEvent", "dataset_123", "deprecated", mock.Anything).Return(nil).Once()

		return &mocks.MockWriter{}, mockDatabase, &mocks.MockBroker{}
	},
	assertMocks: func(t *testing.T, mockReader *mocks.MockWriter, mockDatabase *mocks.MockDatabase, mockBroker *mocks.MockBroker) {
		mockReader.AssertExpectations(t)
		mockDatabase.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	},
	expectedError: nil,
}

var mappingSuccessWithFileDownloadPathsCase = testCase{
	name: "mapping_success_with_download_paths",
	sourceMessage: schema.DatasetMapping{
		Type:         "mapping",
		DatasetID:    "dataset_123",
		AccessionIDs: []string{"file_accession_1", "file_accession_2", "file_accession_3", "file_accession_4", "file_accession_5"},
		FileDownloadPaths: map[string]string{
			"file_accession_1": "file_path_overriden_1",
			"file_accession_2": "file_path_overriden_2",
			"file_accession_3": "file_path_overriden_3",
			"file_accession_4": "file_path_overriden_4",
			"file_accession_5": "file_path_overriden_5",
		},
	},
	newMocks: func(t *testing.T) (*mocks.MockWriter, *mocks.MockDatabase, *mocks.MockBroker) {
		mockWriter := &mocks.MockWriter{}
		mockDatabase := &mocks.MockDatabase{}

		mockDatabase.On("BeginTransaction").Return(nil).Once()
		mockDatabase.On("Rollback").Return(nil).Once()
		mockDatabase.On("Commit").Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_1").Return(&database.MappingData{
			FileID:             "file_id_1",
			User:               "test@user",
			SubmissionFilePath: "/file_path_1/file.c4gh",
			SubmissionLocation: "unit_test_submission_location",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_1", mock.MatchedBy(func(p *string) bool {
			return p != nil && *p == "file_path_overriden_1"
		})).Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_2").Return(&database.MappingData{
			FileID:             "file_id_2",
			User:               "test@user",
			SubmissionFilePath: "/file_path_2/file.c4gh",
			SubmissionLocation: "unit_test_submission_location",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_2", mock.MatchedBy(func(p *string) bool {
			return p != nil && *p == "file_path_overriden_2"
		})).Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_3").Return(&database.MappingData{
			FileID:             "file_id_3",
			User:               "test@user",
			SubmissionFilePath: "/file_path_3/file.c4gh",
			SubmissionLocation: "unit_test_submission_location",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_3", mock.MatchedBy(func(p *string) bool {
			return p != nil && *p == "file_path_overriden_3"
		})).Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_4").Return(&database.MappingData{
			FileID:             "file_id_4",
			User:               "test@user",
			SubmissionFilePath: "/file_path_4/file.c4gh",
			SubmissionLocation: "unit_test_submission_location_other",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_4", mock.MatchedBy(func(p *string) bool {
			return p != nil && *p == "file_path_overriden_4"
		})).Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_5").Return(&database.MappingData{
			FileID:             "file_id_5",
			User:               "test@user",
			SubmissionFilePath: "/file_path_5/file.c4gh",
			SubmissionLocation: "unit_test_submission_location",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_5", mock.MatchedBy(func(p *string) bool {
			return p != nil && *p == "file_path_overriden_5"
		})).Return(nil).Once()

		mockDatabase.On("UpdateDatasetEvent", "dataset_123", "registered", mock.Anything).Return(nil).Once()

		mockWriter.On("RemoveFile", "unit_test_submission_location", "test_user/file_path_1/file.c4gh").Return(nil).Once()
		mockWriter.On("RemoveFile", "unit_test_submission_location", "test_user/file_path_2/file.c4gh").Return(nil).Once()
		mockWriter.On("RemoveFile", "unit_test_submission_location", "test_user/file_path_3/file.c4gh").Return(nil).Once()
		mockWriter.On("RemoveFile", "unit_test_submission_location_other", "test_user/file_path_4/file.c4gh").Return(nil).Once()
		mockWriter.On("RemoveFile", "unit_test_submission_location", "test_user/file_path_5/file.c4gh").Return(nil).Once()

		return mockWriter, mockDatabase, &mocks.MockBroker{}
	},
	assertMocks: func(t *testing.T, mockReader *mocks.MockWriter, mockDatabase *mocks.MockDatabase, mockBroker *mocks.MockBroker) {
		mockReader.AssertExpectations(t)
		mockDatabase.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	},
	expectedError: nil,
}

var unknownOperationCase = testCase{
	name: "unknown_operation",
	sourceMessage: schema.DatasetMapping{
		Type:      "create",
		DatasetID: "dataset_123",
	},
	newMocks: func(t *testing.T) (*mocks.MockWriter, *mocks.MockDatabase, *mocks.MockBroker) {
		mockBroker := &mocks.MockBroker{}

		expectedMessage := schema.DatasetMapping{
			Type:      "create",
			DatasetID: "dataset_123",
		}

		expectedRaw, err := json.Marshal(&expectedMessage)
		if err != nil {
			t.Errorf("failed to marshal expected message: %s", err.Error())
			t.FailNow()
		}

		mockBroker.On("Publish", "error", brokerv2.Message{
			Key: "dataset_123",
			Headers: map[string]any{
				"error-queue-reason": "could not derive schema from message",
			},
			Body: expectedRaw,
		}).Return(nil).Once()

		return &mocks.MockWriter{}, &mocks.MockDatabase{}, mockBroker
	},
	assertMocks: func(t *testing.T, mockReader *mocks.MockWriter, mockDatabase *mocks.MockDatabase, mockBroker *mocks.MockBroker) {
		mockReader.AssertExpectations(t)
		mockDatabase.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	},
	expectedError: nil,
}

var mappingSuccessNoSubmissionLocationsCase = testCase{
	name: "mapping_success_no_submission_locations",
	sourceMessage: schema.DatasetMapping{
		Type:         "mapping",
		DatasetID:    "dataset_123",
		AccessionIDs: []string{"file_accession_1", "file_accession_2", "file_accession_3", "file_accession_4", "file_accession_5"},
	},
	newMocks: func(t *testing.T) (*mocks.MockWriter, *mocks.MockDatabase, *mocks.MockBroker) {
		mockDatabase := &mocks.MockDatabase{}

		mockDatabase.On("BeginTransaction").Return(nil).Once()
		mockDatabase.On("Rollback").Return(nil).Once()
		mockDatabase.On("Commit").Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_1").Return(&database.MappingData{
			FileID:             "file_id_1",
			User:               "test@user",
			SubmissionFilePath: "/file_path_1/file.c4gh",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_1", (*string)(nil)).Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_2").Return(&database.MappingData{
			FileID:             "file_id_2",
			User:               "test@user",
			SubmissionFilePath: "/file_path_2/file.c4gh",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_2", (*string)(nil)).Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_3").Return(&database.MappingData{
			FileID:             "file_id_3",
			User:               "test@user",
			SubmissionFilePath: "/file_path_3/file.c4gh",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_3", (*string)(nil)).Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_4").Return(&database.MappingData{
			FileID:             "file_id_4",
			User:               "test@user",
			SubmissionFilePath: "/file_path_4/file.c4gh",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_4", (*string)(nil)).Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_5").Return(&database.MappingData{
			FileID:             "file_id_5",
			User:               "test@user",
			SubmissionFilePath: "/file_path_5/file.c4gh",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_5", (*string)(nil)).Return(nil).Once()

		mockDatabase.On("UpdateDatasetEvent", "dataset_123", "registered", mock.Anything).Return(nil).Once()

		return &mocks.MockWriter{}, mockDatabase, &mocks.MockBroker{}
	},
	assertMocks: func(t *testing.T, mockReader *mocks.MockWriter, mockDatabase *mocks.MockDatabase, mockBroker *mocks.MockBroker) {
		mockReader.AssertExpectations(t)
		mockDatabase.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	},
	expectedError: nil,
}

var mappingGetMappingDataRetryableErrorCase = testCase{
	name: "mapping_MapFileToDataset_retryable_db_error",
	sourceMessage: schema.DatasetMapping{
		Type:         "mapping",
		DatasetID:    "dataset_123",
		AccessionIDs: []string{"file_accession_1", "file_accession_2", "file_accession_3", "file_accession_4", "file_accession_5"},
	},
	newMocks: func(t *testing.T) (*mocks.MockWriter, *mocks.MockDatabase, *mocks.MockBroker) {
		mockDatabase := &mocks.MockDatabase{}

		mockDatabase.On("BeginTransaction").Return(nil).Once()
		mockDatabase.On("Rollback").Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_1").Return(nil, errors.New("retryable error")).Once()

		return &mocks.MockWriter{}, mockDatabase, &mocks.MockBroker{}
	},
	assertMocks: func(t *testing.T, mockReader *mocks.MockWriter, mockDatabase *mocks.MockDatabase, mockBroker *mocks.MockBroker) {
		mockReader.AssertExpectations(t)
		mockDatabase.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	},
	expectedError: errors.New("retryable error"),
}

var mappingMapFileToDatasetRetryableErrorCase = testCase{
	name: "mapping_MapFileToDataset_retryable_db_error",
	sourceMessage: schema.DatasetMapping{
		Type:         "mapping",
		DatasetID:    "dataset_123",
		AccessionIDs: []string{"file_accession_1", "file_accession_2", "file_accession_3", "file_accession_4", "file_accession_5"},
	},
	newMocks: func(t *testing.T) (*mocks.MockWriter, *mocks.MockDatabase, *mocks.MockBroker) {
		mockDatabase := &mocks.MockDatabase{}

		mockDatabase.On("BeginTransaction").Return(nil).Once()
		mockDatabase.On("Rollback").Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_1").Return(&database.MappingData{
			FileID:             "file_id_1",
			User:               "test@user",
			SubmissionFilePath: "/file_path_1/file.c4gh",
		}, nil).Once()
		mockDatabase.On("MapFileToDataset", "dataset_123", "file_id_1", (*string)(nil)).Return(errors.New("retryable error")).Once()

		return &mocks.MockWriter{}, mockDatabase, &mocks.MockBroker{}
	},
	assertMocks: func(t *testing.T, mockReader *mocks.MockWriter, mockDatabase *mocks.MockDatabase, mockBroker *mocks.MockBroker) {
		mockReader.AssertExpectations(t)
		mockDatabase.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	},
	expectedError: errors.New("retryable error"),
}

var mappingNoMappingDataFoundCase = testCase{
	name: "mapping_no_mapping_data_found",
	sourceMessage: schema.DatasetMapping{
		Type:         "mapping",
		DatasetID:    "dataset_123",
		AccessionIDs: []string{"file_accession_1", "file_accession_2", "file_accession_3", "file_accession_4", "file_accession_5"},
	},
	newMocks: func(t *testing.T) (*mocks.MockWriter, *mocks.MockDatabase, *mocks.MockBroker) {
		mockDatabase := &mocks.MockDatabase{}
		mockBroker := &mocks.MockBroker{}

		mockDatabase.On("BeginTransaction").Return(nil).Once()
		mockDatabase.On("Rollback").Return(nil).Once()

		mockDatabase.On("GetMappingData", "file_accession_1").Return(nil, nil).Once()

		expectedMessage := schema.DatasetMapping{
			Type:         "mapping",
			DatasetID:    "dataset_123",
			AccessionIDs: []string{"file_accession_1", "file_accession_2", "file_accession_3", "file_accession_4", "file_accession_5"},
		}

		expectedRaw, err := json.Marshal(&expectedMessage)
		if err != nil {
			t.Errorf("failed to marshal expected message: %s", err.Error())
			t.FailNow()
		}

		mockBroker.On("Publish", "error", brokerv2.Message{
			Key: "dataset_123",
			Headers: map[string]any{
				"error-queue-reason": "mapping data for file not found",
			},
			Body: expectedRaw,
		}).Return(nil).Once()

		return &mocks.MockWriter{}, mockDatabase, mockBroker
	},
	assertMocks: func(t *testing.T, mockReader *mocks.MockWriter, mockDatabase *mocks.MockDatabase, mockBroker *mocks.MockBroker) {
		mockReader.AssertExpectations(t)
		mockDatabase.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	},
	expectedError: nil,
}
