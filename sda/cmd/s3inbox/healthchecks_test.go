package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/neicnordic/sensitive-data-archive/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestHealthCheck(t *testing.T) {
	for _, tc := range []struct {
		name               string
		clientTimeout      time.Duration
		newMocks           func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer)
		expectedStatusCode int
	}{
		{
			name: "AllGood",
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				mdb := &mocks.MockDatabase{}
				mb := &mocks.MockBroker{}
				ms := &mockServer{}

				mb.On("Alive").Return(true).Once()
				mdb.On("Ping").Return(nil).Once()
				ms.On("ServeHTTP", http.MethodGet, "/ready", "").Return(200, nil, nil).Once()

				return mdb, mb, ms
			},
			expectedStatusCode: http.StatusOK,
		}, {
			name: "broker_bad",
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				mb := &mocks.MockBroker{}

				mb.On("Alive").Return(false).Once()

				return &mocks.MockDatabase{}, mb, &mockServer{}
			},
			expectedStatusCode: http.StatusServiceUnavailable,
		}, {
			name: "database_bad",
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				mdb := &mocks.MockDatabase{}
				mb := &mocks.MockBroker{}

				mb.On("Alive").Return(true).Once()
				mdb.On("Ping").Return(errors.New("connection error")).Once()

				return mdb, mb, &mockServer{}
			},
			expectedStatusCode: http.StatusServiceUnavailable,
		}, {
			name: "s3_bad",
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				mdb := &mocks.MockDatabase{}
				mb := &mocks.MockBroker{}
				ms := &mockServer{}

				mb.On("Alive").Return(true).Once()
				mdb.On("Ping").Return(nil).Once()
				ms.On("ServeHTTP", http.MethodGet, "/ready", "").Return(500, nil, nil).Once()

				return mdb, mb, ms
			},
			expectedStatusCode: http.StatusServiceUnavailable,
		}, {
			name: "db_timeout",
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				mdb := &mocks.MockDatabase{}
				mb := &mocks.MockBroker{}

				mb.On("Alive").Return(true).Once()
				mdb.On("Ping").Run(func(_ mock.Arguments) {
					time.Sleep(1 * time.Second)
				}).Return(context.Canceled).Once()

				return mdb, mb, &mockServer{}
			},
			clientTimeout:      500 * time.Millisecond,
			expectedStatusCode: http.StatusServiceUnavailable,
		}, {
			name: "s3_timeout",
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				mdb := &mocks.MockDatabase{}
				mb := &mocks.MockBroker{}
				ms := &mockServer{}

				mb.On("Alive").Return(true).Once()
				mdb.On("Ping").Return(nil).Once()

				ms.On("ServeHTTP", http.MethodGet, "/ready", "").Run(func(_ mock.Arguments) {
					time.Sleep(1 * time.Second)
				}).Return(500, nil, nil).Once()

				return mdb, mb, ms
			},
			clientTimeout:      500 * time.Millisecond,
			expectedStatusCode: http.StatusServiceUnavailable,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockDatabase, mockBroker, mockS3ServerImpl := tc.newMocks()

			s3MockServer := httptest.NewServer(http.HandlerFunc(mockS3ServerImpl.ServeHTTP))
			defer s3MockServer.Close()

			p := &proxy{
				s3Conf: s3InboxConfig{
					endpoint:  s3MockServer.URL,
					readyPath: "/ready",
				},
				broker:   mockBroker,
				database: mockDatabase,
				client: &http.Client{
					Timeout: tc.clientTimeout,
				},
			}

			w := httptest.NewRecorder()
			p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "/health", nil))

			assert.Equal(t, w.Code, tc.expectedStatusCode)
			mockDatabase.AssertExpectations(t)
			mockBroker.AssertExpectations(t)
			mockS3ServerImpl.AssertExpectations(t)
		})
	}
}
