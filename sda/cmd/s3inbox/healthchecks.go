package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

func (p *Proxy) LivenessHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (p *Proxy) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := p.database.DB.PingContext(ctx); err != nil {
		slog.Error("readiness failed", "reason", "database", "error", err)
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)

		return
	}

	if p.messenger == nil || p.messenger.IsConnClosed() {
		slog.Warn("Readiness failed - Messenger disconnected")
		w.WriteHeader(http.StatusServiceUnavailable)
		slog.Error("readiness failed", "reason", "messenger_dissconnected")
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)

		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Ready"))
}
