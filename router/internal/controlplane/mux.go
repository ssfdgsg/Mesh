package controlplane

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"router/internal/sidercfg"
	"router/internal/stream"
)

// configPublisher loads and broadcasts config updates.
type configPublisher struct {
	mu         sync.Mutex
	loader     sidercfg.Loader
	hub        *stream.Hub
	lastUpdate time.Time
	lastErr    string
}

func (p *configPublisher) loadCanonical() ([]byte, error) {
	raw, err := p.loader.Load()
	if err != nil {
		return nil, err
	}
	cfg, err := sidercfg.Parse(raw)
	if err != nil {
		return nil, err
	}
	b, err := sidercfg.MarshalCanonical(cfg)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (p *configPublisher) loadAndBroadcast(reason string) {
	b, err := p.loadCanonical()
	if err != nil {
		p.mu.Lock()
		p.lastErr = err.Error()
		p.mu.Unlock()
		log.Printf("config load: reason=%s err=%v", reason, err)
		return
	}
	p.mu.Lock()
	p.lastErr = ""
	p.lastUpdate = time.Now()
	p.mu.Unlock()
	p.hub.Broadcast(b)
	log.Printf("config broadcast: reason=%s bytes=%d clients=%d", reason, len(b), p.hub.ClientCount())
}

func NewMux(ctx context.Context, loader sidercfg.Loader, pollInterval time.Duration, uiDir string) http.Handler {
	hub := stream.NewHub()
	p := &configPublisher{loader: loader, hub: hub}

	// Initial config load so the first stream clients can get data immediately.
	p.loadAndBroadcast("init")

	// Polling watcher (minimal, dependency-free).
	go stream.PollFile(ctx, loader, pollInterval, func() {
		p.loadAndBroadcast("poll")
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// SSE stream: push config updates to sider.
	mux.HandleFunc("GET /v1/sider/config/stream", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("sse connect: path=%s remote=%s", r.URL.Path, r.RemoteAddr)
		stream.ServeSSE(r.Context(), w, hub)
		log.Printf("sse disconnect: path=%s remote=%s", r.URL.Path, r.RemoteAddr)
	})

	// Manual trigger: reload from file and broadcast to all streams.
	mux.HandleFunc("POST /v1/sider/config/push", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("config push: remote=%s", r.RemoteAddr)
		p.loadAndBroadcast("push")
		w.WriteHeader(http.StatusNoContent)
	})

	registerUIRoutes(mux, p, hub, pollInterval)
	if uiDir != "" {
		registerUISPA(mux, uiDir)
	}

	return withCORS(mux)
}
