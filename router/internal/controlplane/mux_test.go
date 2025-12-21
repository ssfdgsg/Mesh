package controlplane

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"router/internal/sidercfg"
)

func TestConfigStream_PushOnConnectAndChange(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sider.json")

	writeFile(t, cfgPath, `{"listeners":[{"listen":":18080","upstreams":["127.0.0.1:9000"]}],"dial_timeout_ms":1000}`)

	loader := sidercfg.FileLoader{Path: cfgPath}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	srv := httptest.NewServer(NewMux(ctx, loader, 10*time.Millisecond, ""))
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/sider/config/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %s", resp.Status)
	}

	r := bufio.NewReader(resp.Body)
	got1 := readSSEConfigEvent(t, r, 2*time.Second)

	var c1 sidercfg.Config
	if err := json.Unmarshal(got1, &c1); err != nil {
		t.Fatalf("unmarshal cfg1: %v", err)
	}
	if len(c1.Listeners) != 1 || c1.Listeners[0].Listen != ":18080" {
		t.Fatalf("unexpected cfg1: %+v", c1)
	}

	writeFile(t, cfgPath, `{"listeners":[{"listen":":18081","upstreams":["127.0.0.1:9001"]}],"dial_timeout_ms":1000}`)
	got2 := readSSEConfigEvent(t, r, 2*time.Second)

	var c2 sidercfg.Config
	if err := json.Unmarshal(got2, &c2); err != nil {
		t.Fatalf("unmarshal cfg2: %v", err)
	}
	if len(c2.Listeners) != 1 || c2.Listeners[0].Listen != ":18081" {
		t.Fatalf("unexpected cfg2: %+v", c2)
	}
}

func readSSEConfigEvent(t *testing.T, r *bufio.Reader, timeout time.Duration) []byte {
	t.Helper()
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		var event string
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				ch <- result{err: err}
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue
			}
			if strings.HasPrefix(line, "event:") {
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				continue
			}
			if strings.HasPrefix(line, "data:") && event == "config" {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				// Read the blank line that terminates the event.
				_, _ = r.ReadString('\n')
				ch <- result{data: []byte(data)}
				return
			}
		}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("read: %v", res.err)
		}
		return res.data
	case <-time.After(timeout):
		t.Fatal("timeout waiting for sse event")
		return nil
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}
