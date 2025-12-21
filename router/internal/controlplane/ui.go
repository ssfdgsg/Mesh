package controlplane

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"router/internal/stream"
)

type uiStatus struct {
	OK             bool   `json:"ok"`
	PollIntervalMs int64  `json:"poll_interval_ms"`
	ConfigLoaded   bool   `json:"config_loaded"`
	ConfigUpdated  string `json:"config_updated_at,omitempty"`
	LastError      string `json:"last_error,omitempty"`
}

func registerUIRoutes(mux *http.ServeMux, p *configPublisher, hub *stream.Hub, pollInterval time.Duration) {
	mux.HandleFunc("GET /v1/ui/status", func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		lastUpdate := p.lastUpdate
		lastErr := p.lastErr
		p.mu.Unlock()

		st := uiStatus{
			OK:             true,
			PollIntervalMs: pollInterval.Milliseconds(),
			ConfigLoaded:   !lastUpdate.IsZero(),
			LastError:      lastErr,
		}
		if !lastUpdate.IsZero() {
			st.ConfigUpdated = lastUpdate.UTC().Format(time.RFC3339Nano)
		}
		writeJSON(w, http.StatusOK, st)
	})

	mux.HandleFunc("GET /v1/ui/config", func(w http.ResponseWriter, r *http.Request) {
		if b, ok := hub.Last(); ok {
			writeRawJSON(w, http.StatusOK, b)
			return
		}
		p.loadAndBroadcast("ui_fetch")
		if b, ok := hub.Last(); ok {
			writeRawJSON(w, http.StatusOK, b)
			return
		}
		http.Error(w, "config unavailable", http.StatusServiceUnavailable)
	})

	mux.HandleFunc("POST /v1/ui/config/reload", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("ui config reload: remote=%s", r.RemoteAddr)
		p.loadAndBroadcast("ui_reload")
		if b, ok := hub.Last(); ok {
			writeRawJSON(w, http.StatusOK, b)
			return
		}
		http.Error(w, "config unavailable", http.StatusServiceUnavailable)
	})

	// Alias for UI consumption (same payload as sider stream).
	mux.HandleFunc("GET /v1/ui/config/stream", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("sse connect: path=%s remote=%s", r.URL.Path, r.RemoteAddr)
		stream.ServeSSE(r.Context(), w, hub)
		log.Printf("sse disconnect: path=%s remote=%s", r.URL.Path, r.RemoteAddr)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRawJSON(w http.ResponseWriter, status int, b []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}
