package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/model"
)

const (
	readValidationResultsQuery     = "readValidationResults"
	readValidationInformationQuery = "readValidationInformation"
	insertFileValidationJobQuery   = "insertFileValidationJob"
	updateFileValidationJobQuery   = "updateFileValidationJob"
	allValidationJobsDoneQuery     = "allValidationJobsDone"
)

var queries = map[string]string{
	readValidationResultsQuery: `
SELECT validator_id, validator_result, validator_messages, started_at, finished_at, file_path, file_result, file_messages
FROM file_validation_job
WHERE validation_id = $1
AND ($2::text IS NULL OR $2::text = submission_user)`,

	readValidationInformationQuery: `
SELECT validation_id, file_id, file_path, submission_file_size, validator_id, submission_user
FROM file_validation_job
WHERE validation_id = $1
AND validator_result = 'pending'`,

	insertFileValidationJobQuery: `
INSERT INTO file_validation_job(validation_id, validator_id, file_id, file_path, submission_file_size, submission_user, triggered_by, started_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,

	updateFileValidationJobQuery: `
UPDATE file_validation_job SET
finished_at = $1, file_result = $2, validator_messages = $3, file_messages = $4, validator_result = $5
WHERE file_id = $6
AND validator_id = $7
AND validation_id = $8`,

	allValidationJobsDoneQuery: `
SELECT false
FROM file_validation_job
WHERE validation_id = $1
AND finished_at IS NULL`,
}

func (db *pgDb) readValidationResult(ctx context.Context, stmt *sql.Stmt, validationID string, userID *string) (*model.ValidationResult, error) {
	rows, err := stmt.QueryContext(ctx, validationID, userID)
	defer func() {
		_ = rows.Close()
	}()
	if err != nil {
		return nil, err
	}

	validatorResults := make(map[string]*model.ValidatorResult)

	for rows.Next() {
		var validatorMessages, fileMessages, startedAt, finishedAt string
		fileResult := new(model.FileResult)
		validatorResult := new(model.ValidatorResult)

		if err := rows.Scan(
			&validatorResult.ValidatorID,
			&validatorResult.Result,
			&validatorMessages,
			&startedAt,
			&finishedAt,
			&fileResult.FilePath,
			&fileResult.Result,
			&fileMessages); err != nil {
			return nil, err
		}

		if err := json.Unmarshal([]byte(fileMessages), &fileResult.Messages); err != nil {
			return nil, fmt.Errorf("failed to unmarshal file messages: %v", err)
		}

		if readValidatorResult, ok := validatorResults[validatorResult.ValidatorID]; ok {
			readValidatorResult.Files = append(readValidatorResult.Files, fileResult)
			continue
		}

		if err := json.Unmarshal([]byte(validatorMessages), &validatorResult.Messages); err != nil {
			return nil, fmt.Errorf("failed to unmarshal file messages: %v", err)
		}

		validatorResult.StartedAt, err = time.Parse(time.RFC3339, startedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse started at: %v", err)
		}
		validatorResult.FinishedAt, err = time.Parse(time.RFC3339, startedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse started at: %v", err)
		}

		validatorResults[validatorResult.ValidatorID] = validatorResult
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Check if any rows where found, if not return nil, nil to indicate validation result not found
	if len(validatorResults) == 0 {
		return nil, nil
	}
	validationResult := &model.ValidationResult{
		ValidationID:     validationID,
		ValidatorResults: make([]*model.ValidatorResult, 0, len(validatorResults)),
	}
	for _, validatorResult := range validatorResults {
		validationResult.ValidatorResults = append(validationResult.ValidatorResults, validatorResult)
	}

	return validationResult, nil
}

func (db *pgDb) readValidationInformation(ctx context.Context, stmt *sql.Stmt, validationID string) (*model.ValidationInformation, error) {
	rows, err := stmt.QueryContext(ctx, validationID)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	validationInformation := new(model.ValidationInformation)
	validatorsIDs := make(map[string]struct{})

	for rows.Next() {
		fileInformation := new(model.FileInformation)
		var validatorsID string

		if err := rows.Scan(
			&validationInformation.ValidationID,
			&fileInformation.FileID,
			&fileInformation.FilePath,
			&fileInformation.SubmissionFileSize,
			&validatorsID,
			&validationInformation.SubmissionUserID); err != nil {
			return nil, err
		}

		validatorsIDs[validatorsID] = struct{}{}
		validationInformation.Files = append(validationInformation.Files, fileInformation)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Check if any rows where found, if not return nil, nil to indicate no validation information not found
	if len(validatorsIDs) == 0 {
		return nil, nil
	}

	for validatorID := range validatorsIDs {
		validationInformation.ValidatorIDs = append(validationInformation.ValidatorIDs, validatorID)
	}

	return validationInformation, nil
}

func (db *pgDb) insertFileValidationJob(ctx context.Context, stmt *sql.Stmt, validationID, validatorID, fileID, filePath string, fileSubmissionSize int64, submissionUser, triggeredBy string, startedAt time.Time) error {
	if _, err := stmt.ExecContext(ctx, validationID, validatorID, fileID, filePath, fileSubmissionSize, submissionUser, triggeredBy, startedAt.Format(time.RFC3339)); err != nil {
		return err
	}
	return nil
}

func (db *pgDb) updateFileValidationJob(ctx context.Context, stmt *sql.Stmt, validationID, validatorID, fileID, fileResult string, fileMessages []*model.Message, finishedAt time.Time, validatorResult string, validatorMessages []*model.Message) error {
	fm, err := json.Marshal(fileMessages)
	if err != nil {
		return fmt.Errorf("failed to marshal file messages: %v", err)
	}
	vm, err := json.Marshal(validatorMessages)
	if err != nil {
		return fmt.Errorf("failed to marshal validator messages: %v", err)
	}

	if _, err := stmt.ExecContext(ctx, finishedAt.Format(time.RFC3339), fileResult, vm, fm, validatorResult, fileID, validatorID, validationID); err != nil {
		return err
	}
	return nil
}

func (db *pgDb) allValidationJobsDone(ctx context.Context, stmt *sql.Stmt, validationID string) (bool, error) {
	row := stmt.QueryRowContext(ctx, validationID)

	if err := row.Err(); err != nil {
		// If we got any rows, there are still pending jobs
		if errors.Is(row.Err(), sql.ErrNoRows) {
			return true, nil
		}
		return false, err
	}

	return false, nil
}
