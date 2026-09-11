package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestHandleMessage(t *testing.T) {
	type testCase struct {
		name          string
		message       any
		routing       map[messageType]string
		newMock       func(testCase) *mocks.MockBroker
		expectedError error
	}

	for _, tc := range []testCase{
		{
			name: "accession",
			message: &schema.IngestionAccession{
				Type:               "accession",
				User:               "123",
				FilePath:           "321",
				AccessionID:        "",
				DecryptedChecksums: nil,
			},
			newMock: func(tc testCase) *mocks.MockBroker {
				mb := &mocks.MockBroker{}

				expectedMsgBody, _ := json.Marshal(tc.message)

				mb.On("Publish", "accession_rk", mock.MatchedBy(func(msg broker.Message) bool {
					return bytes.Equal(msg.Body, expectedMsgBody) && msg.Key == "accession_test_case"
				})).Return(nil).Once()

				return mb
			},
			routing: map[messageType]string{
				"accession": "accession_rk",
			},
			expectedError: nil,
		}, {
			name: "ingestion",
			message: &schema.IngestionTrigger{
				Type:     "ingest",
				User:     "123",
				FilePath: "321",
			},
			newMock: func(tc testCase) *mocks.MockBroker {
				mb := &mocks.MockBroker{}

				expectedMsgBody, _ := json.Marshal(tc.message)

				mb.On("Publish", "ingest_rk", mock.MatchedBy(func(msg broker.Message) bool {
					return bytes.Equal(msg.Body, expectedMsgBody) && msg.Key == "ingestion_test_case"
				})).Return(nil).Once()

				return mb
			},
			routing: map[messageType]string{
				"ingest": "ingest_rk",
			},
			expectedError: nil,
		}, {
			name: "cancel",
			message: &schema.IngestionTrigger{
				Type:     "cancel",
				User:     "123",
				FilePath: "321",
			},
			newMock: func(tc testCase) *mocks.MockBroker {
				mb := &mocks.MockBroker{}

				expectedMsgBody, _ := json.Marshal(tc.message)

				mb.On("Publish", "cancel_rk", mock.MatchedBy(func(msg broker.Message) bool {
					return bytes.Equal(msg.Body, expectedMsgBody) && msg.Key == "cancel_test_case"
				})).Return(nil).Once()

				return mb
			},
			routing: map[messageType]string{
				"cancel": "cancel_rk",
			},
			expectedError: nil,
		}, {
			name: "type_no_rk",
			message: &schema.IngestionTrigger{
				Type: "no_rk",
			},
			newMock: func(tc testCase) *mocks.MockBroker {
				mb := &mocks.MockBroker{}

				expectedMsgBody, _ := json.Marshal(tc.message)

				mb.On("Publish", "undeliverable", mock.MatchedBy(func(msg broker.Message) bool {
					return bytes.Equal(msg.Body, expectedMsgBody) && msg.Key == "type_no_rk_test_case"
				})).Return(nil).Once()

				return mb
			},
			routing: map[messageType]string{
				"cancel": "cancel_rk",
			},
			expectedError: nil,
		}, {
			name: "empty_type",
			message: &schema.IngestionTrigger{
				Type: "",
			},
			newMock: func(tc testCase) *mocks.MockBroker {
				mb := &mocks.MockBroker{}

				expectedMsgBody, _ := json.Marshal(tc.message)

				mb.On("Publish", "undeliverable", mock.MatchedBy(func(msg broker.Message) bool {
					return bytes.Equal(msg.Body, expectedMsgBody) && msg.Key == "empty_type_test_case"
				})).Return(nil).Once()

				return mb
			},
			routing: map[messageType]string{
				"cancel": "cancel_rk",
			},
			expectedError: nil,
		}, {
			name: "inc_msg_no_type",
			message: &struct {
				NoType string `json:"no_type"`
			}{
				NoType: "no_type",
			},
			newMock: func(tc testCase) *mocks.MockBroker {
				return &mocks.MockBroker{}
			},
			routing:       map[messageType]string{},
			expectedError: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockBroker := tc.newMock(tc)

			app := intercept{
				broker:  mockBroker,
				routing: tc.routing,
			}

			msgBody, _ := json.Marshal(tc.message)
			callBacks, err := app.handleMessage(context.Background(), &broker.Message{
				Key:     tc.name + "_test_case",
				Headers: nil,
				Body:    msgBody,
			})
			for _, cb := range callBacks {
				cb()
			}

			assert.Equal(t, tc.expectedError, err, "error does not match expected")
			mockBroker.AssertExpectations(t)
		})
	}
}
func TestTypeFromMessage(t *testing.T) {
	for _, tc := range []struct {
		name                string
		message             any
		expectedMessageType messageType
		expectedError       error
	}{
		{
			name: "accession",
			message: &schema.IngestionAccession{
				Type: "accession",
			},
			expectedMessageType: messageTypeAccession,
			expectedError:       nil,
		}, {
			name: "cancel",
			message: &schema.IngestionTrigger{
				Type: "cancel",
			},
			expectedMessageType: messageTypeCancel,
			expectedError:       nil,
		}, {
			name: "ingest",
			message: &schema.IngestionTrigger{
				Type: "ingest",
			},
			expectedMessageType: messageTypeIngest,
			expectedError:       nil,
		}, {
			name: "mapping",
			message: &schema.DatasetMapping{
				Type: "mapping",
			},
			expectedMessageType: messageTypeMapping,
			expectedError:       nil,
		}, {
			name: "deprecate",
			message: &schema.DatasetMapping{
				Type: "deprecate",
			},
			expectedMessageType: messageTypeDeprecate,
			expectedError:       nil,
		}, {
			name: "release",
			message: &schema.DatasetMapping{
				Type: "release",
			},
			expectedMessageType: messageTypeRelease,
			expectedError:       nil,
		}, {
			name: "no_type",
			message: &struct {
				NoType string `json:"no_type"`
			}{
				NoType: "other_type",
			},
			expectedMessageType: "",
			expectedError:       errors.New("malformed message, type is missing"),
		}, {
			name: "empty_type",
			message: &struct {
				Type string `json:"type"`
			}{},
			expectedMessageType: "",
			expectedError:       nil,
		}, {
			name: "wrong_type_type",
			message: &struct {
				Type int `json:"type"`
			}{
				Type: 1,
			},
			expectedMessageType: "",
			expectedError:       errors.New("could not cast type attribute to string"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message, _ := json.Marshal(tc.message)

			msgType, err := typeFromMessage(message)

			assert.Equal(t, tc.expectedError, err, "error does not match expected")
			assert.Equal(t, tc.expectedMessageType, msgType, "message type from message does not match expected")
		})
	}
}
