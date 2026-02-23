package handlers

import (
	"github.com/neicnordic/sensitive-data-archive/cmd/download/audit"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/database"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/reencrypt"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/visa"
	storage "github.com/neicnordic/sensitive-data-archive/internal/storage/v2"
)

// WithDatabase sets the database for the handlers.
func WithDatabase(db database.Database) func(*Handlers) {
	return func(h *Handlers) {
		h.db = db
	}
}

// WithStorageReader sets the storage reader for the handlers.
func WithStorageReader(reader storage.Reader) func(*Handlers) {
	return func(h *Handlers) {
		h.storageReader = reader
	}
}

// WithReencryptClient sets the reencrypt client for the handlers.
func WithReencryptClient(client *reencrypt.Client) func(*Handlers) {
	return func(h *Handlers) {
		h.reencryptClient = client
	}
}

// WithVisaValidator sets the GA4GH visa validator for the handlers.
func WithVisaValidator(v *visa.Validator) func(*Handlers) {
	return func(h *Handlers) {
		h.visaValidator = v
	}
}

// WithAuditLogger sets the audit logger for the handlers.
func WithAuditLogger(l audit.Logger) func(*Handlers) {
	return func(h *Handlers) {
		h.auditLogger = l
	}
}

// WithGRPCReencryptHost sets the gRPC reencrypt service host (deprecated, use WithReencryptClient).
func WithGRPCReencryptHost(host string) func(*Handlers) {
	return func(h *Handlers) {
		h.grpcHost = host
	}
}

// WithGRPCReencryptPort sets the gRPC reencrypt service port (deprecated, use WithReencryptClient).
func WithGRPCReencryptPort(port int) func(*Handlers) {
	return func(h *Handlers) {
		h.grpcPort = port
	}
}
