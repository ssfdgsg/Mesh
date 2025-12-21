package controlplane

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	// BaseURL is like "http://127.0.0.1:8081".
	BaseURL string
	// Node is an optional node id added as ?node=...
	Node string
	HTTP *http.Client
}

func (c Client) StreamConfigs(ctx context.Context) (<-chan []byte, <-chan error) {
	out := make(chan []byte, 16)
	errs := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errs)

		backoff := 1 * time.Second
		for {
			if ctx.Err() != nil {
				return
			}
			if err := c.streamOnce(ctx, out); err != nil && !errors.Is(err, context.Canceled) {
				select {
				case errs <- err:
				default:
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
		}
	}()

	return out, errs
}

func (c Client) streamOnce(ctx context.Context, out chan<- []byte) error {
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{}
	}

	u, err := url.Parse(strings.TrimRight(c.BaseURL, "/"))
	if err != nil {
		return fmt.Errorf("parse base url: %w", err)
	}
	u.Path = u.Path + "/v1/sider/config/stream"
	if c.Node != "" {
		q := u.Query()
		q.Set("node", c.Node)
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("control plane: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}

	return readSSE(resp.Body, func(event string, data []byte) {
		if event != "config" || len(data) == 0 {
			return
		}
		select {
		case out <- bytes.Clone(data):
		default:
			// Drop if the consumer is too slow.
		}
	})
}

func readSSE(r io.Reader, onEvent func(event string, data []byte)) error {
	s := bufio.NewScanner(r)
	// Allow up to 2MB per event line.
	s.Buffer(make([]byte, 0, 64*1024), 2<<20)

	var event string
	var dataLines [][]byte

	flush := func() {
		if len(dataLines) == 0 {
			event = ""
			return
		}
		data := bytes.Join(dataLines, []byte("\n"))
		onEvent(event, data)
		event = ""
		dataLines = dataLines[:0]
	}

	for s.Scan() {
		line := s.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			b := []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			dataLines = append(dataLines, b)
			continue
		}
	}
	if err := s.Err(); err != nil {
		return err
	}
	return io.EOF
}
