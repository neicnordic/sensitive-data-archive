package main

import (
	"context"
	"log/slog"
	"net/http"
)

func (p *Proxy) LivenessHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (p *Proxy) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	if err := p.database.DB.PingContext(ctx); err != nil {
		slog.Error("readiness failed", "reason", "database", "error", err)
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)

		return
	}

	if p.messenger == nil || p.messenger.IsConnClosed() {
		slog.Error("readiness failed", "reason", "messenger")
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)

		return
	}

	resp, err := p.client.Get(p.s3Conf.ReadyPath)
	if err != nil {
		slog.Error("readiness failed", "reason", "s3")
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)

		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("readiness faield", "service", "s3", "reason", resp.Status)
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)

		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Ready"))
}
