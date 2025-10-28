package database

import (
	"context"
	"time"

	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/model"
)

type Transaction interface {
	// Commit the transaction
	Commit() error
	// Rollback the transaction
	Rollback() error
	functions
}

type Database interface {
	// BeginTransaction starts a database transaction, either commit or rollback needs to be called when done to release resources and close transaction
	BeginTransaction(ctx context.Context) (Transaction, error)
	// Close the database connection
	Close() error
	functions
}

// functions denotes the available database functions
type functions interface {
	// ReadValidationResult reads the validation results by a validationID and optionally a userID to check that the files validated belongs to the user
	ReadValidationResult(ctx context.Context, validationID string, userID *string) (*model.ValidationResult, error)
	// ReadValidationInformation returns the pending validator jobs for a validation
	ReadValidationInformation(ctx context.Context, validationID string) (*model.ValidationInformation, error)

	InsertFileValidationJob(ctx context.Context, validationID, validatorID, fileID, filePath string, fileSubmissionSize int64, submissionUser, triggeredBy string, startedAt time.Time) error
	// UpdateFileValidationJob updates a file validation jobs with
	UpdateFileValidationJob(ctx context.Context, validationID, validatorID, fileID, fileResult string, fileMessages []*model.Message, finishedAt time.Time, validatorResult string, validatorMessages []*model.Message) error
	// AllValidationJobsDone checks if all validator jobs for a validation have finished
	AllValidationJobsDone(ctx context.Context, validationID string) (bool, error)
}

var db Database

// RegisterDatabase registers the database implementation to be used
func RegisterDatabase(d Database) {
	db = d
}

// BeginTransaction starts a database transaction, either commit or rollback needs to be called when done to release resources and close transaction
func BeginTransaction(ctx context.Context) (Transaction, error) {
	return db.BeginTransaction(ctx)
}

// ReadValidationResult reads the validation results by a validationID and optionally a userID to check that the files validated belongs to the user
func ReadValidationResult(ctx context.Context, validationID string, userID *string) (*model.ValidationResult, error) {
	return db.ReadValidationResult(ctx, validationID, userID)
}

// ReadValidationInformation returns the pending validator jobs for a validation
func ReadValidationInformation(ctx context.Context, validationID string) (*model.ValidationInformation, error) {
	return db.ReadValidationInformation(ctx, validationID)
}

// AllValidationJobsDone checks if all validator jobs for a validation have finished
func AllValidationJobsDone(ctx context.Context, validationID string) (bool, error) {
	return db.AllValidationJobsDone(ctx, validationID)
}

// Close the database connection
func Close() error {
	return db.Close()
}
