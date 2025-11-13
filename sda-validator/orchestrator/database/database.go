package database

import (
	"context"

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

	// InsertFileValidationJob inserts a file validation job
	InsertFileValidationJob(ctx context.Context, insertFileValidationJobParameters *model.InsertFileValidationJobParameters) error
	// UpdateFileValidationJob updates a file validation jobs with
	UpdateFileValidationJob(ctx context.Context, fileValidationJobUpdateParameters *model.UpdateFileValidationJobParameters) error
	// UpdateAllValidationJobFilesOnError updates the result of all validation_file_jobs to error by the validation id, with the validator message to provide details
	UpdateAllValidationJobFilesOnError(ctx context.Context, validationID string, validatorMessage *model.Message) error
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

// UpdateAllValidationJobFilesOnError updates the result of all validation_file_jobs to error by the validation id, with the validator message to provide details
func UpdateAllValidationJobFilesOnError(ctx context.Context, validationId string, validatorMessage *model.Message) error {
	return db.UpdateAllValidationJobFilesOnError(ctx, validationId, validatorMessage)
}

// Close the database connection
func Close() error {
	return db.Close()
}
