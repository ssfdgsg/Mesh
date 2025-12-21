package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"router/internal/sidercfg"
)

func TestUIConfig_GetAndReload(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sider.json")

	writeFile(t, cfgPath, `{"listeners":[{"listen":":18080","upstreams":["127.0.0.1:9000"]}],"dial_timeout_ms":1000}`)
	loader := sidercfg.FileLoader{Path: cfgPath}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	srv := httptest.NewServer(NewMux(ctx, loader, 10*time.Second, ""))
	t.Cleanup(srv.Close)

	getCfg := func() sidercfg.Config {
		resp, err := http.Get(srv.URL + "/v1/ui/config")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status: %s", resp.Status)
		}
		var cfg sidercfg.Config
		if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return cfg
	}

	cfg1 := getCfg()
	if len(cfg1.Listeners) != 1 || cfg1.Listeners[0].Listen != ":18080" {
		t.Fatalf("unexpected cfg1: %+v", cfg1)
	}

	writeFile(t, cfgPath, `{"listeners":[{"listen":":18081","upstreams":["127.0.0.1:9001"]}],"dial_timeout_ms":1000}`)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/ui/config/reload", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %s", resp.Status)
	}
	var cfg2 sidercfg.Config
	if err := json.NewDecoder(resp.Body).Decode(&cfg2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(cfg2.Listeners) != 1 || cfg2.Listeners[0].Listen != ":18081" {
		t.Fatalf("unexpected cfg2: %+v", cfg2)
	}
}

func TestCORS_Preflight(t *testing.T) {
	t.Parallel()

	loader := sidercfg.FileLoader{Path: filepath.Join(t.TempDir(), "missing.json")}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	srv := httptest.NewServer(NewMux(ctx, loader, 10*time.Second, ""))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodOptions, srv.URL+"/v1/ui/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status: %s", resp.Status)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got == "" {
		t.Fatalf("missing cors header")
	}
}
