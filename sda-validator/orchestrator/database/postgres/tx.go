package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/model"
)

type pgTx struct {
	tx *sql.Tx
	pgDb
}

func (tx *pgTx) Commit() error {
	return tx.tx.Commit()
}

func (tx *pgTx) Rollback() error {
	err := tx.tx.Rollback()
	if errors.Is(err, sql.ErrTxDone) {
		return nil
	}

	return err
}

func (tx *pgTx) ReadValidationResult(ctx context.Context, validationID string, userID *string) (*model.ValidationResult, error) {
	return tx.readValidationResult(ctx, tx.tx.Stmt(preparedStatements[readValidationResultsQuery]), validationID, userID)
}

func (tx *pgTx) ReadValidationInformation(ctx context.Context, validationID string) (*model.ValidationInformation, error) {
	return tx.readValidationInformation(ctx, tx.tx.Stmt(preparedStatements[readValidationInformationQuery]), validationID)
}

func (tx *pgTx) InsertFileValidationJob(ctx context.Context, validationID, validatorID, fileID, filePath string, fileSubmissionSize int64, submissionUser, triggeredBy string, startedAt time.Time) error {
	return tx.insertFileValidationJob(ctx, tx.tx.Stmt(preparedStatements[insertFileValidationJobQuery]), validationID, validatorID, fileID, filePath, fileSubmissionSize, submissionUser, triggeredBy, startedAt)
}

func (tx *pgTx) UpdateFileValidationJob(ctx context.Context, validationID, validatorID, fileID, fileResult string, fileMessages []*model.Message, finishedAt time.Time, validatorResult string, validatorMessages []*model.Message) error {
	return tx.updateFileValidationJob(ctx, tx.tx.Stmt(preparedStatements[updateFileValidationJobQuery]), validationID, validatorID, fileID, fileResult, fileMessages, finishedAt, validatorResult, validatorMessages)
}

func (tx *pgTx) AllValidationJobsDone(ctx context.Context, validationID string) (bool, error) {
	return tx.allValidationJobsDone(ctx, tx.tx.Stmt(preparedStatements[allValidationJobsDoneQuery]), validationID)
}
