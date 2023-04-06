// Package database provides functionalities for using the database,
// providing high level functions
package database

import (
	"errors"
)

// RegisterFile inserts a file in the database, along with a "registered" log
// event. If the file already exists in the database, the entry is updated, but
// a new file event is always inserted.
func (dbs *SDAdb) RegisterFile(uploadPath, uploadUser string) (string, error) {

	dbs.checkAndReconnectIfNeeded()

	if dbs.Version < 4 {
		return "", errors.New("database schema v4 required for RegisterFile()")
	}

	query := "SELECT sda.register_file($1, $2)"

	var fileID string

	err := dbs.DB.QueryRow(query, uploadPath, uploadUser).Scan(&fileID)

	return fileID, err
}

// MarkFileAsUploaded updates a file that is currently "registered" to
// "uploaded" to show that a file has finished uploading. The message parameter
// is the rabbitmq message sent on file upload.
func (dbs *SDAdb) MarkFileAsUploaded(fileID, userID, message string) error {

	dbs.checkAndReconnectIfNeeded()

	if dbs.Version < 4 {
		return errors.New("database schema v4 required for MarkFileAsUploaded()")
	}

	query := "INSERT INTO sda.file_event_log(file_id, event, user_id, message) VALUES ($1, 'uploaded', $2, $3)"

	_, err := dbs.DB.Exec(query, fileID, userID, message)

	return err
}
