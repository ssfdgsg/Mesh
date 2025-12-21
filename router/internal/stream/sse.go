package stream

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"time"
)

// ServeSSE 建立推送长连接Server-Sent Events
func ServeSSE(ctx context.Context, w http.ResponseWriter, hub *Hub) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	msgCh := make(chan []byte, 16)
	hub.Add(msgCh)
	defer hub.Remove(msgCh)

	bw := bufio.NewWriter(w)
	write := func(event string, data []byte) error {
		if _, err := fmt.Fprintf(bw, "event: %s\n", event); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(bw, "data: %s\n\n", data); err != nil {
			return err
		}
		if err := bw.Flush(); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()

	// Best-effort initial keepalive so proxies don't buffer.
	_, _ = bw.WriteString(":ok\n\n")
	_ = bw.Flush()
	flusher.Flush()

	for {
		select {
		case <-ctx.Done():
			return
		case <-keepAlive.C:
			_, _ = bw.WriteString(":keepalive\n\n")
			_ = bw.Flush()
			flusher.Flush()
		case msg := <-msgCh:
			if err := write("config", msg); err != nil {
				return
			}
		}
	}
}
