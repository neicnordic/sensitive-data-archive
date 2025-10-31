package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/lib/pq" // Import pg driver
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/database"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/model"
	log "github.com/sirupsen/logrus"
)

type pgDb struct {
	db     *sql.DB
	config *dbConfig
}

var preparedStatements map[string]*sql.Stmt

func Init(options ...func(config *dbConfig)) error {
	dbConf := globalConf.clone()

	for _, o := range options {
		o(dbConf)
	}

	pg := &pgDb{db: nil, config: dbConf}

	var err error
	pg.db, err = sql.Open("postgres", pg.config.dataSourceName())
	if err != nil {
		return fmt.Errorf("failed to connect to database: %v", err)
	}

	if err := pg.db.Ping(); err != nil {
		return fmt.Errorf("failed to connect to database: %v", err)
	}

	// Prepare the statements from the queries
	preparedStatements = make(map[string]*sql.Stmt)
	for queryName, query := range queries {
		preparedStmt, err := pg.db.Prepare(query)
		if err != nil {
			log.Errorf("failed to prepare query: %s, due to: %v", queryName, err)

			return errors.Join(fmt.Errorf("failed to prepare query: %s", queryName), err)
		}
		preparedStatements[queryName] = preparedStmt
	}

	database.RegisterDatabase(pg)

	return nil
}

// Close terminates the connection to the database
func (db *pgDb) Close() error {
	if db.db == nil {
		return nil
	}

	return db.db.Close()
}

func (db *pgDb) BeginTransaction(ctx context.Context) (database.Transaction, error) {
	tx, err := db.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	return &pgTx{tx: tx}, nil
}

func (db *pgDb) ReadValidationResult(ctx context.Context, validationID string, userID *string) (*model.ValidationResult, error) {
	return db.readValidationResult(ctx, preparedStatements[readValidationResultsQuery], validationID, userID)
}

func (db *pgDb) ReadValidationInformation(ctx context.Context, validationID string) (*model.ValidationInformation, error) {
	return db.readValidationInformation(ctx, preparedStatements[readValidationInformationQuery], validationID)
}

func (db *pgDb) InsertFileValidationJob(ctx context.Context, insertFileValidationJobParameters *model.InsertFileValidationJobParameters) error {
	return db.insertFileValidationJob(ctx, preparedStatements[insertFileValidationJobQuery], insertFileValidationJobParameters)
}

func (db *pgDb) UpdateFileValidationJob(ctx context.Context, updateFileValidationJobParameters *model.UpdateFileValidationJobParameters) error {
	return db.updateFileValidationJob(ctx, preparedStatements[updateFileValidationJobQuery], updateFileValidationJobParameters)
}

func (db *pgDb) AllValidationJobsDone(ctx context.Context, validationID string) (bool, error) {
	return db.allValidationJobsDone(ctx, preparedStatements[allValidationJobsDoneQuery], validationID)
}
